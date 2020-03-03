#!/bin/bash

# set pvc count
PVC_COUNT=50

# test pvc
rm -rf /root/pfy/performance-pvc
mkdir -p /root/pfy/performance-pvc

for((i=1;i<=${PVC_COUNT};i++));  
do
	file="/root/pfy/performance-pvc/pvc-""${i}"".yaml"
	cat > $file <<EOF

apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-test-$i
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  storageClassName: csi-sc-arstorplugin

EOF

done  

kubectl create -f /root/pfy/performance-pvc/.

