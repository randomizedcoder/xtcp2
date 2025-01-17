# clickhouse k8s

## nodes on k3s

```
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
sudo chown root:wheel /etc/rancher/k3s/k3s.yaml && sudo chmod 640 /etc/rancher/k3s/k3s.yaml
```

https://github.com/Altinity/clickhouse-operator/blob/master/docs/chk-examples/02-extended-3-nodes.yaml

https://blog.duyet.net/2024/03/clickhouse-on-kubernetes.html


helm repo add clickhouse-operator https://docs.altinity.com/clickhouse-operator
helm upgrade --install --create-namespace \
    --namespace clickhouse \
    clickhouse-operator \
    clickhouse-operator/altinity-clickhouse-operator

```

[das@hp1:~]$ helm upgrade --install --create-namespace \
    --namespace clickhouse \
    clickhouse-operator \
    clickhouse-operator/altinity-clickhouse-operator
WARNING: Kubernetes configuration file is group-readable. This is insecure. Location: /etc/rancher/k3s/k3s.yaml
Release "clickhouse-operator" does not exist. Installing it now.
NAME: clickhouse-operator
LAST DEPLOYED: Wed Dec 25 08:36:49 2024
NAMESPACE: clickhouse
STATUS: deployed
REVISION: 1
TEST SUITE: None
```

```
[das@hp1:~]$ kubectl apply -f dave-02-extended-3-nodes.yaml
clickhousekeeperinstallation.clickhouse-keeper.altinity.com/extended created
```

```
[das@hp1:~]$ kubectl get clickhousekeeperinstallation.clickhouse-keeper.altinity.com/extended -n clickhouse
NAME       CLUSTERS   HOSTS   STATUS      HOSTS-COMPLETED   AGE
extended   1          3       Completed                     28h
```

```
[das@hp1:~]$ host redpanda-0.redpanda.redpanda.svc.cluster.local 10.43.0.10
Using domain server:
Name: 10.43.0.10
Address: 10.43.0.10#53
Aliases:

redpanda-0.redpanda.redpanda.svc.cluster.local has address 10.42.2.65
```


```
[das@hp1:~]$ host chk-extended-cluster1-0-0.clickhouse.svc.cluster.local 10.43.0.10
Using domain server:
Name: 10.43.0.10
Address: 10.43.0.10#53
Aliases:

chk-extended-cluster1-0-0.clickhouse.svc.cluster.local has address 10.42.0.85
```

```
[das@hp1:~]$ kubectl describe clickhousekeeperinstallation.clickhouse-keeper.altinity.com/extended -n clickhouse | grep -A 3 Fqdns
  Fqdns:
    chk-extended-cluster1-0-0.clickhouse.svc.cluster.local
    chk-extended-cluster1-0-1.clickhouse.svc.cluster.local
    chk-extended-cluster1-0-2.clickhouse.svc.cluster.local
```

```
[das@hp1:~]$ kubectl get ClickhouseInstallation -n clickhouse
NAME              CLUSTERS   HOSTS   STATUS       HOSTS-COMPLETED   AGE
clickhouse-inst   1          3       InProgress                     27h
```


```
[das@hp1:~]$ kubectl cluster-info
Kubernetes control plane is running at https://127.0.0.1:6443
CoreDNS is running at https://127.0.0.1:6443/api/v1/namespaces/kube-system/services/kube-dns:dns/proxy
Metrics-server is running at https://127.0.0.1:6443/api/v1/namespaces/kube-system/services/https:metrics-server:https/proxy

To further debug and diagnose cluster problems, use 'kubectl cluster-info dump'.
```

https://kubernetes.io/docs/reference/kubectl/quick-reference/

https://kb.altinity.com/altinity-kb-kubernetes/

redpanda-0.redpanda.redpanda.svc.cluster.local:9093


kubectl exec --stdin --tty chi-clickhouse-inst-clickhouse-0-0-0 -n clickhouse -- /bin/bash
kubectl exec --stdin --tty chi-clickhouse-inst-clickhouse-0-1-0 -n clickhouse -- /bin/bash

kubectl -n clickhouse describe chi


To nuke namesapce

NS=`kubectl get ns |grep Terminating | awk 'NR==1 {print $1}'` && kubectl get namespace "$NS" -o json   | tr -d "\n" | sed "s/\"finalizers\": \[[^]]\+\]/\"finalizers\": []/"   | kubectl replace --raw /api/v1/namespaces/$NS/finalize -f -


....


```
[das@hp1:~]$ helm upgrade --install --create-namespace \
    --namespace clickhouse \
    clickhouse-operator \
    clickhouse-operator/altinity-clickhouse-operator
Release "clickhouse-operator" does not exist. Installing it now.
NAME: clickhouse-operator
LAST DEPLOYED: Fri Dec 27 12:37:48 2024
NAMESPACE: clickhouse
STATUS: deployed
REVISION: 1
TEST SUITE: None
```

```
kubectl -n clickhouse describe chk
```

kubectl delete pod chi-clickhouse-inst-clickhouse-0-0-0 --grace-period=0 --force -n clickhouse


for p in $(kubectl get pods | grep Terminating | awk '{print $1}'); do kubectl delete pod $p --grace-period=0 --force;done
