package k8sutil

import (
	"fmt"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/kube-etcd-controller/pkg/backup"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/resource"
	unversionedAPI "k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/apis/storage"
	"k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/util/wait"
)

const (
	storageClassName = "etcd-controller-backup"
)

func CreateStorageClass(kubecli *unversioned.Client, pvProvisioner string) error {
	class := &storage.StorageClass{
		ObjectMeta: api.ObjectMeta{
			Name: storageClassName,
		},
		Provisioner: pvProvisioner,
	}
	_, err := kubecli.StorageClasses().Create(class)
	return err
}

func createAndWaitPVC(kubecli *unversioned.Client, clusterName, ns string, volumeSizeInMB int) (*api.PersistentVolumeClaim, error) {
	claim := &api.PersistentVolumeClaim{
		ObjectMeta: api.ObjectMeta{
			Name: fmt.Sprintf("pvc-%s", clusterName),
			Labels: map[string]string{
				"etcd_cluster": clusterName,
			},
			Annotations: map[string]string{
				"volume.beta.kubernetes.io/storage-class": storageClassName,
			},
		},
		Spec: api.PersistentVolumeClaimSpec{
			AccessModes: []api.PersistentVolumeAccessMode{
				api.ReadWriteOnce,
			},
			Resources: api.ResourceRequirements{
				Requests: api.ResourceList{
					api.ResourceStorage: resource.MustParse(fmt.Sprintf("%dMi", volumeSizeInMB)),
				},
			},
		},
	}
	retClaim, err := kubecli.PersistentVolumeClaims(ns).Create(claim)
	if err != nil {
		return nil, err
	}

	err = wait.Poll(2*time.Second, 10*time.Second, func() (bool, error) {
		claim, err := kubecli.PersistentVolumeClaims(ns).Get(retClaim.Name)
		if err != nil {
			return false, err
		}
		logrus.Infof("PV claim (%s) status.phase: %v", claim.Name, claim.Status.Phase)
		if claim.Status.Phase != api.ClaimBound {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		// TODO: remove retClaim
		logrus.Errorf("fail to poll PVC (%s): %v", retClaim.Name, err)
		return nil, err
	}

	return retClaim, nil
}

func CreateBackupReplicaSetAndService(kubecli *unversioned.Client, clusterName, ns string, policy backup.Policy) error {
	claim, err := createAndWaitPVC(kubecli, clusterName, ns, policy.VolumeSizeInMB)
	if err != nil {
		return err
	}

	labels := map[string]string{
		"app":          "etcd_backup_tool",
		"etcd_cluster": clusterName,
	}
	name := makeBackupName(clusterName)
	_, err = kubecli.ReplicaSets(ns).Create(&extensions.ReplicaSet{
		ObjectMeta: api.ObjectMeta{
			Name: name,
		},
		Spec: extensions.ReplicaSetSpec{
			Replicas: 1,
			Selector: &unversionedAPI.LabelSelector{MatchLabels: labels},
			Template: api.PodTemplateSpec{
				ObjectMeta: api.ObjectMeta{
					Labels: labels,
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Name:  "backup",
							Image: "gcr.io/coreos-k8s-scale-testing/kube-etcd-backup:latest",
							Command: []string{
								"backup",
								"--etcd-cluster",
								clusterName,
							},
							Env: []api.EnvVar{{
								Name:      "MY_POD_NAMESPACE",
								ValueFrom: &api.EnvVarSource{FieldRef: &api.ObjectFieldSelector{FieldPath: "metadata.namespace"}},
							}},
							VolumeMounts: []api.VolumeMount{{
								Name:      "etcd-backup-storage",
								MountPath: backup.BackupDir,
							}},
						},
					},
					Volumes: []api.Volume{{
						Name: "etcd-backup-storage",
						VolumeSource: api.VolumeSource{
							PersistentVolumeClaim: &api.PersistentVolumeClaimVolumeSource{
								ClaimName: claim.Name,
							},
						},
					}},
				},
			},
		},
	})
	if err != nil {
		return err
	}

	svc := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: api.ServiceSpec{
			Ports: []api.ServicePort{
				{
					Name:       "backup-service",
					Port:       19999, // default port
					TargetPort: intstr.FromInt(19999),
					Protocol:   api.ProtocolTCP,
				},
			},
			Selector: labels,
		},
	}
	if _, err := kubecli.Services(ns).Create(svc); err != nil {
		return err
	}
	return nil
}