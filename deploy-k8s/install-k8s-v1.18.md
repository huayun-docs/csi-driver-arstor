在安超节点上使用kubeadm安装kubernetes


1.安装必要的rpm软件：
yum install -y wget vim net-tools epel-release

2.配置k8s源：
cat <<EOF > /etc/yum.repos.d/kubernetes.repo
[kubernetes]
name=Kubernetes
baseurl=https://mirrors.aliyun.com/kubernetes/yum/repos/kubernetes-el7-x86_64
enabled=1
gpgcheck=0
EOF
## 重建yum缓存
yum clean all
yum makecache fast

3.确认docker版本
# docker -v
Docker version 18.09.0, build 4d60db4

4.安装kubelet和kubeadm
curl -sSL https://kuboard.cn/install-script/v1.18.x/init_master.sh | sh -s 1.18.1

yum list kubeadm --showduplicates | grep 1.18.1
yum install -y kubeadm-1.18.1-0 kubelet-1.18.1-0 kubectl-1.18.1-0

5.获取需要的镜像
# kubeadm config images list 

k8s.gcr.io/kube-apiserver:v1.18.1
k8s.gcr.io/kube-controller-manager:v1.18.1
k8s.gcr.io/kube-scheduler:v1.18.1
k8s.gcr.io/kube-proxy:v1.18.1
k8s.gcr.io/pause:3.2
k8s.gcr.io/etcd:3.4.3-0
k8s.gcr.io/coredns:1.6.7

6.下载镜像
```
# vim kubeadm.sh

#!/bin/bash

## 使用如下脚本下载国内镜像，并修改tag为google的tag
set -e

KUBE_VERSION=v1.18.1
KUBE_PAUSE_VERSION=3.2
ETCD_VERSION=3.4.3-0
CORE_DNS_VERSION=1.6.7

GCR_URL=k8s.gcr.io
ALIYUN_URL=registry.cn-hangzhou.aliyuncs.com/google_containers

images=(kube-proxy:${KUBE_VERSION}
kube-scheduler:${KUBE_VERSION}
kube-controller-manager:${KUBE_VERSION}
kube-apiserver:${KUBE_VERSION}
pause:${KUBE_PAUSE_VERSION}
etcd:${ETCD_VERSION}
coredns:${CORE_DNS_VERSION})

for imageName in ${images[@]} ; do
  docker pull $ALIYUN_URL/$imageName
  docker tag  $ALIYUN_URL/$imageName $GCR_URL/$imageName
  docker rmi $ALIYUN_URL/$imageName
done
```
sh ./kubeadm.sh

7.安装master节点
增加配置文件
APISERVER_ADDR="10.130.176.17"
POD_SUBNET="10.244.0.0/16 "
cat <<EOF > kubeadm-config.yaml
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: v1.18.1
imageRepository: registry.cn-hangzhou.aliyuncs.com/google_containers
controlPlaneEndpoint: "${APISERVER_ADDR}:6443"
networking:
  serviceSubnet: "10.96.0.0/16"
  podSubnet: "${POD_SUBNET}"
  dnsDomain: "cluster.local"
EOF

# kubeadm init --config=kubeadm-config.yaml --upload-certs

8.设置
mkdir -p $HOME/.kube
  sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
  sudo chown $(id -u):$(id -g) $HOME/.kube/config


9.安装node节点
You can now join any number of the control-plane node running the following command on each as root:

  kubeadm join 10.130.176.17:6443 --token f6ip2a.k85mthoranx2ltpg \
    --discovery-token-ca-cert-hash sha256:b6d20b14a3359603c413214b7e7ccdf7c8bc48638a7b78cc72752497cada874a \
    --control-plane --certificate-key 29454a14703353739c212a515ee7b9819eeca455f40edee82235952f07c67590

Please note that the certificate-key gives access to cluster sensitive data, keep it secret!
As a safeguard, uploaded-certs will be deleted in two hours; If necessary, you can use
"kubeadm init phase upload-certs --upload-certs" to reload certs afterward.

Then you can join any number of worker nodes by running the following on each as root:

kubeadm join 10.130.176.17:6443 --token f6ip2a.k85mthoranx2ltpg \
    --discovery-token-ca-cert-hash sha256:b6d20b14a3359603c413214b7e7ccdf7c8bc48638a7b78cc72752497cada874a 


10.安装flanneld

wget https://raw.githubusercontent.com/coreos/flannel/master/Documentation/kube-flannel.yml
kubectl apply -f kube-flanneld.yml
