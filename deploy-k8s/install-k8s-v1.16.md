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
yum list kubeadm --showduplicates | grep 1.16
yum install -y kubeadm-1.16.1 kubelet-1.16.1

5.获取需要的镜像
# kubeadm config images list 

k8s.gcr.io/kube-apiserver:v1.16.1
k8s.gcr.io/kube-controller-manager:v1.16.1
k8s.gcr.io/kube-scheduler:v1.16.1
k8s.gcr.io/kube-proxy:v1.16.1
k8s.gcr.io/pause:3.1
k8s.gcr.io/etcd:3.3.15-0
k8s.gcr.io/coredns:1.6.2

6.下载镜像
# vim kubeadm.sh

#!/bin/bash

## 使用如下脚本下载国内镜像，并修改tag为google的tag
set -e

KUBE_VERSION=v1.16.1
KUBE_PAUSE_VERSION=3.1
ETCD_VERSION=3.3.15-0
CORE_DNS_VERSION=1.6.2

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

sh ./kubeadm.sh

7.安装master节点
# kubeadm init --apiserver-advertise-address 172.16.4.45 --kubernetes-version=v1.16.1 --pod-network-cidr=10.244.0.0/16 --ignore-preflight-errors='Port-2379,Port-2380,Swap,DirAvailable--var-lib-etcd'

8.如果是要安装多个master节点，则初始化命令使用

kubeadm init  --apiserver-advertise-address 192.168.10.20 --control-plane-endpoint 192.168.10.20  --kubernetes-version=v1.16.0  --pod-network-cidr=10.244.0.0/16  --upload-certs

添加master节点使用命令:

kubeadm join 192.168.10.20:6443 --token z34zii.ur84appk8h9r3yik --discovery-token-ca-cert-hash sha256:dae426820f2c6073763a3697abeb14d8418c9268288e37b8fc25674153702801     --control-plane --certificate-key 1b9b0f1fdc0959a9decef7d812a2f606faf69ca44ca24d2e557b3ea81f415afe

9.安装node节点

kubeadm join 192.168.10.20:6443 --token lixsl8.v1auqmf91ty0xl0k \
    --discovery-token-ca-cert-hash sha256:c3f92a6ed9149ead327342f48a545e7e127a455d5b338129feac85893d918a55 


10.安装flanneld
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config

wget https://raw.githubusercontent.com/coreos/flannel/master/Documentation/kube-flannel.yml
kubectl apply -f kube-flanneld.yml
