# preparation:
kubectl create clusterrolebinding initializer-admin --clusterrole=cluster-admin --serviceaccount=default:default


# real
cat example/deployment.yaml
kubectl create -f example/deployment.yaml
kubectl get pod
kubectl get -w crd
grep "syncing monitors.*etcd.database.coreos.com" /tmp/kube-controller-manager.log
cat example/example-etcd-cluster.yaml
kubectl create -f example/example-etcd-cluster.yaml
kubectl get etcdclusters
kubectl get pods
kubectl get pod example-etcd-cluster-0000 -o yaml | head -n 20
kubectl get services
kubectl get service example-etcd-cluster -o yaml | head -n 20
kubectl get service example-etcd-cluster-client -o yaml | head -n 20
kubectl delete etcdclusters example-etcd-cluster
kubectl get pods
kubectl get services

grep "delete object.*etcd" /tmp/kube-controller-manager.log
