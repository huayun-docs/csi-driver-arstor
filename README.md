ArStor是一个[华云](https://www.huayun.com/)的分布式存储系统，它是基于[Maxta](https://www.stsginc.com/2017/Maxta%20Technical%20White%20Paper%200417.pdf)打造而成；
ArStor能提供具有高可靠性, 高性能的存储服务，csi-driver-arstor是基于CSI机制为ArStor开发的driver，使得kubernetes能通过volume manager使用ArStor volume，利用ArStor的存储功能。

## 使用文档

当前文档适应于kubernetes v1.16+,对老版本kubernetes需要增加以下配置：
```
Enable flag --allow-privileged=true for kubelet and kube-apiserver
Enable kube-apiserver feature gates --feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,CSIBlockVolume=true,VolumeSnapshotDataSource=true,VolumePVCDataSource=true
Enable kubelet feature gates --feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,CSIBlockVolume=true
```

# 一、编译插件
```
$ make push 
mkdir -p bin
CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-X main.version=915690febb932cd51b612f6b51f10aff3cf9b647 -extldflags "-static"' -o ./bin/arstorplugin ./cmd/arstorplugin
sudo docker build -t arstorplugin:latest -f Dockerfile --label revision=915690febb932cd51b612f6b51f10aff3cf9b647 .
[sudo] password for panfy: 
Sending build context to Docker daemon  281.5MB
Step 1/7 : FROM alpine
 ---> 965ea09ff2eb
Step 2/7 : LABEL maintainers="panfengyun"
 ---> Using cache
 ---> 301391e0ceca
Step 3/7 : LABEL description="ArStor Driver"
 ---> Using cache
 ---> c91356830f0a
Step 4/7 : RUN apk add util-linux multipath-tools e2fsprogs xfsprogs file
 ---> Using cache
 ---> 602b29a1c657
Step 5/7 : COPY ./bin/arstorplugin /arstorplugin
 ---> fd969ad78af5
Step 6/7 : ENTRYPOINT ["/arstorplugin"]
 ---> Running in 250a1dd17fcc
Removing intermediate container 250a1dd17fcc
 ---> 544cab0fa24b
Step 7/7 : LABEL revision=915690febb932cd51b612f6b51f10aff3cf9b647
 ---> Running in f90d32d3ce6d
Removing intermediate container f90d32d3ce6d
 ---> 916054595431
Successfully built 916054595431
Successfully tagged arstorplugin:latest
set -ex; \
push_image () { \
	sudo docker tag arstorplugin:latest docker.io/fengyunpan/arstorplugin:$tag; \
	sudo docker push docker.io/fengyunpan/arstorplugin:$tag; \
}; \
       for tag in v1.0.0; do \
	push_image; \
       done
+ for tag in v1.0.0
+ push_image
+ sudo docker tag arstorplugin:latest docker.io/fengyunpan/arstorplugin:v1.0.0
+ sudo docker push docker.io/fengyunpan/arstorplugin:v1.0.0
The push refers to repository [docker.io/fengyunpan/arstorplugin]
22c1e6c88617: Pushed 
21b3caa3cebb: Layer already exists 
77cae8ab23bf: Layer already exists 
v1.0.0: digest: sha256:528de791aefb849fb46cc64635eac1aed3d709858e97bcdd8284f6bd301658ec size: 951
```
注：编译后自动打包生成一个docker镜像，该镜像上传到了docker.io仓库，镜像默认为：docker.io/fengyunpan/arstorplugin:v1.0.0，如有需要请更改Makefile的配置

## 二、安装

以kubernetes-1.16为例：
1.根据kubernetes版本到deploy/kubernetes-1.16目录下，修改插件的镜像地址:
```
$ grep -rn arstorplugin:v1.0.0 deploy/kubernetes-1.16/
deploy/kubernetes-1.16/arstor-csi-controllerplugin.yaml:84:          image: docker.io/fengyunpan/arstorplugin:v1.0.0
deploy/kubernetes-1.16/arstor-csi-nodeplugin.yaml:53:          image: docker.io/fengyunpan/arstorplugin:v1.0.0
```
2.参考deploy/README.md配置yaml文件

3.在kubernetes环境上安装
```
$ kubectl create -f deploy/kubernetes-1.16/.
configmap/arstor-configmap created
serviceaccount/csi-arstor-controller-sa created
clusterrole.rbac.authorization.k8s.io/csi-attacher-role created
clusterrolebinding.rbac.authorization.k8s.io/csi-attacher-binding created
clusterrole.rbac.authorization.k8s.io/csi-provisioner-role created
clusterrolebinding.rbac.authorization.k8s.io/csi-provisioner-binding created
clusterrole.rbac.authorization.k8s.io/csi-snapshotter-role created
clusterrolebinding.rbac.authorization.k8s.io/csi-snapshotter-binding created
clusterrole.rbac.authorization.k8s.io/csi-resizer-role created
clusterrolebinding.rbac.authorization.k8s.io/csi-resizer-binding created
role.rbac.authorization.k8s.io/external-resizer-cfg created
rolebinding.rbac.authorization.k8s.io/csi-resizer-role-cfg created
service/csi-arstor-controller-service created
statefulset.apps/csi-arstor-controllerplugin created
csidriver.storage.k8s.io/arstor.csi.huayun.io created
serviceaccount/csi-arstor-node-sa created
clusterrole.rbac.authorization.k8s.io/csi-nodeplugin-role created
clusterrolebinding.rbac.authorization.k8s.io/csi-nodeplugin-binding created
daemonset.apps/csi-arstor-nodeplugin created
```

4.检查pod是否正常启动
```
$ kubectl get pod -n kube-system | grep csi
csi-arstor-controllerplugin-0       5/5     Running   0          51s
csi-arstor-nodeplugin-vpj24         2/2     Running   0          51s
```

## 三、创建并使用
创建一个busybox pod，该pod挂载一个pvc
```
$ kubectl create -f examples/busybox.yaml
storageclass.storage.k8s.io/csi-sc-arstorplugin created
persistentvolumeclaim/csi-pvc-arstorplugin created
pod/busybox created
```

## 四、检查状态

1.volume正常创建
```
$ kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                         STORAGECLASS         REASON   AGE
pvc-ec163e9e-10f7-4aa4-ad06-f343f1fa5887   1Gi        RWO            Delete           Bound    default/csi-pvc-arstorplugin   csi-sc-arstorplugin            65s
$ kubectl get pvc
NAME                  STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
csi-pvc-arstorplugin   Bound    pvc-ec163e9e-10f7-4aa4-ad06-f343f1fa5887   1Gi        RWO            csi-sc-arstorplugin   68s
```

2.arstor文件正常创建
```
$ ls -lh /arstor/kubernetes/volumes/volume_995/cce447b7-0542-11ea-bb8a-d6b82c82f38e_pvc-ec163e9e-10f7-4aa4-ad06-f343f1fa5887 
-rw-r--r-- 1 root root 1.0G Nov 12 19:51 /arstor/kubernetes/volumes/volume_995/cce447b7-0542-11ea-bb8a-d6b82c82f38e_pvc-ec163e9e-10f7-4aa4-ad06-f343f1fa5887
```
注：根据pvc名字到/arstor/kubernetes/目录下查找,例：# find /arstor/kubernetes -name "*pvc-ec163e9e-10f7-4aa4-ad06-f343f1fa5887*"

3.busybox使用了该volume,在上面创建/mnt/test/hello文件，并写下“Hello world!”
```
$ kubectl logs busybox
-rw-r--r--    1 root     root            13 Nov 12 11:51 /mnt/test/hello
Hello world!
```

4.删除
```
$kubectl delete -f examples/busybox.yaml
```

## 五、resize volume

1.创建资源：
```
$ kubectl create -f examples/resize/example.yaml
```

2.volume正常创建
```
$ kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                         STORAGECLASS         REASON   AGE
pvc-b7396a8d-ede2-4889-818e-26a8d527329b   1Gi        RWO            Delete           Bound    default/csi-pvc-arstorplugin   csi-sc-arstorplugin            10s
$ kubectl get pvc
NAME                  STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
csi-pvc-arstorplugin   Bound    pvc-b7396a8d-ede2-4889-818e-26a8d527329b   1Gi        RWO            csi-sc-arstorplugin   12s
```

3.arstor文件正常创建
```
$ find /arstor/kubernetes -name  "*pvc-b7396a8d-ede2-4889-818e-26a8d527329b*"
/arstor/kubernetes/volumes/volume_965/851746ec-0684-11ea-9665-2ad8f3f8f148_pvc-b7396a8d-ede2-4889-818e-26a8d527329b
$ ls -lh /arstor/kubernetes/volumes/volume_965/851746ec-0684-11ea-9665-2ad8f3f8f148_pvc-b7396a8d-ede2-4889-818e-26a8d527329b
-rw-r--r-- 1 root root 1.0G Nov 14 10:15 /arstor/kubernetes/volumes/volume_965/851746ec-0684-11ea-9665-2ad8f3f8f148_pvc-b7396a8d-ede2-4889-818e-26a8d527329b
```

4.busybox使用了该volume,在上面创建/mnt/test/hello文件，并写下“Hello world!”
```
$ kubectl logs busybox
-rw-r--r--    1 root     root            13 Nov 12 11:51 /mnt/test/hello
Hello world!
```

5.修改pvc的大小
```
$ cat examples/resize/example.yaml | grep 'storage:'
      storage: 2Gi
$ kubectl apply -f examples/resize/example.yaml
```

6.volume size发生变更
```
$ kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                         STORAGECLASS         REASON   AGE
pvc-b7396a8d-ede2-4889-818e-26a8d527329b   2Gi        RWO            Delete           Bound    default/csi-pvc-arstorplugin   csi-sc-arstorplugin            3m1s
$ kubectl get pvc
NAME                  STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
csi-pvc-arstorplugin   Bound    pvc-b7396a8d-ede2-4889-818e-26a8d527329b   2Gi        RWO            csi-sc-arstorplugin   3m2s
```

7. arstor文件大小发生变更
```
ls -lh /arstor/kubernetes/volumes/volume_965/851746ec-0684-11ea-9665-2ad8f3f8f148_pvc-b7396a8d-ede2-4889-818e-26a8d527329b
-rw-r--r-- 1 root root 2.0G Nov 14 10:17 /arstor/kubernetes/volumes/volume_965/851746ec-0684-11ea-9665-2ad8f3f8f148_pvc-b7396a8d-ede2-4889-818e-26a8d527329b
```

8.容器内设备大小发生变更
```
 df -lh | grep pvc-b7396a8d-ede2-4889-818e-26a8d527329b
/dev/loop0      2.0G   33M  2.0G   2% /var/lib/kubelet/plugins/kubernetes.io/csi/pv/pvc-b7396a8d-ede2-4889-818e-26a8d527329b
```

9.删除
```
kubectl delete -f examples/busybox.yaml
```


## 六、创建block设备

1.创建资源：
```
 kubectl create -f examples/block/example.yaml
 ```

2.volume正常创建
```
 kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                         STORAGECLASS         REASON   AGE
pvc-e1d6d295-eeca-4236-9c16-c8db20231819   1Gi        RWO            Delete           Bound    default/csi-pvc-arstorplugin   csi-sc-arstorplugin            4s
 kubectl get pvc
NAME                  STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
csi-pvc-arstorplugin   Bound    pvc-e1d6d295-eeca-4236-9c16-c8db20231819   1Gi        RWO            csi-sc-arstorplugin   6s
```

3.arstor文件正常创建
```
 find /arstor/kubernetes -name  "*pvc-e1d6d295-eeca-4236-9c16-c8db20231819*"
/arstor/kubernetes/volumes/volume_915/84a98aa0-06ae-11ea-8eb8-da846c7f5580_pvc-e1d6d295-eeca-4236-9c16-c8db20231819
 ls -h /arstor/kubernetes/volumes/volume_915/84a98aa0-06ae-11ea-8eb8-da846c7f5580_pvc-e1d6d295-eeca-4236-9c16-c8db20231819
/arstor/kubernetes/volumes/volume_915/84a98aa0-06ae-11ea-8eb8-da846c7f5580_pvc-e1d6d295-eeca-4236-9c16-c8db20231819
 ls -lh /arstor/kubernetes/volumes/volume_915/84a98aa0-06ae-11ea-8eb8-da846c7f5580_pvc-e1d6d295-eeca-4236-9c16-c8db20231819
-rw-r--r-- 1 root root 1.0G Nov 14 15:15 /arstor/kubernetes/volumes/volume_915/84a98aa0-06ae-11ea-8eb8-da846c7f5580_pvc-e1d6d295-eeca-4236-9c16-c8db20231819
```

4.busybox能看到该块设备：
```
 kubectl logs busybox
brw-rw----    1 root     disk        7,   0 Nov 14 07:18 /dev/xvda
```

## 七、创建snapshot

1.创建volume
```
 kubectl create -f examples/snapshot/example.yaml
storageclass.storage.k8s.io/csi-sc-arstorplugin created
volumesnapshotclass.snapshot.storage.k8s.io/csi-arstor-snapclass created
persistentvolumeclaim/pvc-snapshot-demo created
 kubectl get pvc
NAME                STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
pvc-snapshot-demo   Bound    pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91   1Gi        RWO            csi-sc-arstorplugin   6s
kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                       STORAGECLASS         REASON   AGE
pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91   1Gi        RWO            Delete           Bound    default/pvc-snapshot-demo   csi-sc-arstorplugin            8s
 find /arstor/kubernetes -name  "*pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91*"
/arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
 ls -lh /arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
-rw-r--r-- 1 root root 1.0G Nov 14 15:23 /arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
```

2.snapshot
```
 kubectl create -f snapshot.yaml 
volumesnapshot.snapshot.storage.k8s.io/new-snapshot-demo created
 kubectl get volumesnapshot
NAME                AGE
new-snapshot-demo   16s
 kubectl get volumesnapshot new-snapshot-demo -o yaml | grep uid
  uid: d17625d7-ab47-45fb-8b62-2f8ea064c701
 find /arstor/kubernetes/ -name "*d17625d7-ab47-45fb-8b62-2f8ea064c701*"
/arstor/kubernetes/snapshots/snapshot_380/24e89de5-06d8-11ea-ae76-56519d7ff67c_snapshot-d17625d7-ab47-45fb-8b62-2f8ea064c701
 ls -lh /arstor/kubernetes/snapshots/snapshot_380/24e89de5-06d8-11ea-ae76-56519d7ff67c_snapshot-d17625d7-ab47-45fb-8b62-2f8ea064c701
-rw-r--r-- 1 root root 1.0G Nov 14 20:13 /arstor/kubernetes/snapshots/snapshot_380/24e89de5-06d8-11ea-ae76-56519d7ff67c_snapshot-d17625d7-ab47-45fb-8b62-2f8ea064c701
```

## 八、restore snapshot
修改kube-apiserver的配置，增加以下参数，并重启kube-apiserver：
--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,CSIBlockVolume=true,VolumeSnapshotDataSource=true,VolumePVCDataSource=true

1.步骤参考七，创建volume和snapshost
```
volume上写入数据
 kubectl get pod 
NAME              READY   STATUS    RESTARTS   AGE
busybox           1/1     Running   0          5s
 kubectl logs busybox 
-rw-r--r--    1 root     root            13 Nov 15 01:19 /mnt/test/hello
Hello world!
snapshot也创建完毕
 kubectl get volumesnapshot
NAME                AGE
new-snapshot-demo   16s
不支持restore正在使用的volume，需删除该pod
 kubectl delete pod busybox
 ```

2.执行restore
```
 kubectl create -f examples/snapshot/snapshotrestore.yaml
```

3.查看结果
```
 kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                           STORAGECLASS         REASON   AGE
pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91   1Gi        RWO            Delete           Bound    default/pvc-snapshot-demo       csi-sc-arstorplugin            19h
pvc-c194b73d-2d64-499c-af3c-8a38336b578d   1Gi        RWO            Delete           Bound    default/snapshot-demo-restore   csi-sc-arstorplugin            3m36s
 kubectl get pvc
NAME                    STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
pvc-snapshot-demo       Bound    pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91   1Gi        RWO            csi-sc-arstorplugin   19h
snapshot-demo-restore   Bound    pvc-c194b73d-2d64-499c-af3c-8a38336b578d   1Gi        RWO            csi-sc-arstorplugin   3m38s
```

4.查看对应文件
```
 find /arstor/kubernetes/ -name "*pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91*"
/arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
 ls -lh /arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
-rw-r--r-- 1 root root 1.0G Nov 15 09:23 /arstor/kubernetes/volumes/volume_714/b8076209-06af-11ea-8eb8-da846c7f5580_pvc-943f30ff-5b7b-4bec-ade3-ea8fe3a25a91
 find /arstor/kubernetes/ -name "*pvc-c194b73d-2d64-499c-af3c-8a38336b578d*"
/arstor/kubernetes/volumes/volume_371/fe9134a8-0751-11ea-8c34-626b08c983d7_pvc-c194b73d-2d64-499c-af3c-8a38336b578d
 ls -lh /arstor/kubernetes/volumes/volume_371/fe9134a8-0751-11ea-8c34-626b08c983d7_pvc-c194b73d-2d64-499c-af3c-8a38336b578d
-rw-r--r-- 1 root root 1.0G Nov 15 10:45 /arstor/kubernetes/volumes/volume_371/fe9134a8-0751-11ea-8c34-626b08c983d7_pvc-c194b73d-2d64-499c-af3c-8a38336b578d
```

5.查看restore出来的volume的使用情况
```
  kubectl get pod 
NAME              READY   STATUS    RESTARTS   AGE
busybox           1/1     Running   0          92m
busybox-restore   1/1     Running   0          6m25s
 kubectl logs busybox-restore 
-rw-r--r--    1 root     root            13 Nov 15 02:45 /mnt/test/hello
Hello world!
restore demo
```

## 九、克隆volume

修改kube-apiserver的配置，增加以下参数，并重启kube-apiserver：
--feature-gates=CSINodeInfo=true,CSIDriverRegistry=true,CSIBlockVolume=true,VolumeSnapshotDataSource=true,VolumePVCDataSource=true

1.创建arstor volume 并让busybox容器去使用
```
 kubectl create -f examples/busybox.yaml
storageclass.storage.k8s.io/csi-sc-arstorplugin created
persistentvolumeclaim/csi-pvc-arstorplugin created
pod/busybox created
```
2.检查volume的数据
```
 kubectl get pod 
NAME      READY   STATUS    RESTARTS   AGE
busybox   1/1     Running   0          49s
 kubectl logs busybox
-rw-r--r--    1 root     root            13 Nov 15 03:02 /mnt/test/hello
Hello world!
 kubectl delete busybox
注： 不支持clone正在使用的volume
3.clone volume
 kubectl create -f examples/clone/clone.yaml
persistentvolumeclaim/pvc-demo-clone created
pod/busybox-clone created
```

4.查看volume
```
 kubectl get pv
NAME                                       CAPACITY   ACCESS MODES   RECLAIM POLICY   STATUS   CLAIM                         STORAGECLASS         REASON   AGE
pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8   1Gi        RWO            Delete           Bound    default/pvc-demo-clone        csi-sc-arstorplugin            19s
pvc-4faebf29-44fd-480e-8ae8-b98e75189c29   1Gi        RWO            Delete           Bound    default/csi-pvc-arstorplugin   csi-sc-arstorplugin            150m
 kubectl get pvc
NAME                  STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS         AGE
csi-pvc-arstorplugin   Bound    pvc-4faebf29-44fd-480e-8ae8-b98e75189c29   1Gi        RWO            csi-sc-arstorplugin   150m
pvc-demo-clone        Bound    pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8   1Gi        RWO            csi-sc-arstorplugin   21s
```

5.查看文件
```
 find /arstor/kubernetes/ -name "*pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8*"
/arstor/kubernetes/volumes/volume_759/68e18eeb-0769-11ea-963d-52b0b5e772c2_pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8
 ls -lh /arstor/kubernetes/volumes/volume_759/68e18eeb-0769-11ea-963d-52b0b5e772c2_pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8 
-rw-r--r-- 1 root root 1.0G Nov 15 13:34 /arstor/kubernetes/volumes/volume_759/68e18eeb-0769-11ea-963d-52b0b5e772c2_pvc-35fa6699-6e12-4e0c-bdc7-50217bc680f8
```

6.busybox使用的情况
```
 kubectl get pod 
NAME            READY   STATUS    RESTARTS   AGE
busybox         1/1     Running   0          151m
busybox-clone   1/1     Running   0          95s
 kubectl logs busybox-clone 
-rw-r--r--    1 root     root            24 Nov 15 10:03 /mnt/test/hello
Hello world!
clone demo
```

## 十、临时volume

1.创建
```
 kubectl create -f ~/pfy/ephemeral.yaml
pod/busybox-ephemeral created
```

2.查看pod使用情况
```
 kubectl get pod 
NAME                READY   STATUS    RESTARTS   AGE
busybox-ephemeral   1/1     Running   0          2m5s
 kubectl logs busybox-ephemeral
-rw-r--r--    1 root     root            13 Nov 18 12:04 /mnt/test/hello
Hello world!
```

3.查看临时volume
```
  losetup  -l
NAME       SIZELIMIT OFFSET AUTOCLEAR RO BACK-FILE
/dev/loop5         0      0         0  0 /arstor/kubernetes/volumes/volume_845/csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb_ephemeral-csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb
 ls -lh /arstor/kubernetes/volumes/volume_845/csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb_ephemeral-csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb
-rw-r--r-- 1 root root 1.0G Nov 18 20:04 /arstor/kubernetes/volumes/volume_845/csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb_ephemeral-csi-a74f142328a253dd99713a1644ac83533394df0a0773a2a7a4e3779bb4e752cb
```

4.删除
```
 kubectl delete -f ~/pfy/ephemeral.yaml 
pod "busybox-ephemeral" deleted
 losetup  -l | wc -l
0
```

