#!/bin/bash

# set statefulset count
COUNT=30

rm -rf /root/pfy/performance-statefulset
mkdir -p /root/pfy/performance-statefulset

for((i=1;i<=${COUNT};i++));
do
	file="/root/pfy/performance-statefulset/statefulset-""${i}"".yaml"
        cat > $file <<EOF

apiVersion: apps/v1
kind: StatefulSet
metadata:
 name: statefulset-arstor-$i
spec:
 serviceName: "nginx"
 replicas: 1
 selector:
  matchLabels:
   app: nginx
 template:
  metadata:
   labels:
    app: nginx
  spec:
   containers:
   - name: nginx
     image: nginx
     ports:
     - containerPort: 80
       name: web
     volumeMounts:
     - name: www
       mountPath: /usr/share/nginx/html
 volumeClaimTemplates:
  - metadata:
     name: www
    spec:
     storageClassName: csi-sc-arstorplugin
     accessModes:
     - ReadWriteOnce
     resources:
      requests:
       storage: 1Gi

EOF

done

kubectl create -f /root/pfy/performance-statefulset/.
