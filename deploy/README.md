# CSI ArStor

## Usage:

## 配置文件

添加配置项：

```
arstorMountPoint = /arstor
arstorShares = 10.10.20.31:/hyperconverged
arstorContainer = mxsp

```

参数说明：

* arstorMountPoint: arstor共享文件夹本地mount文件夹
* arstorShares：arstor共享文件夹网络路径
* arstorContainer: arstor所在的容器名称或者ID


## deploy

kubectl apply -f kubernetes-x.xx/.