性能测试

一、测试50个pvc
1.创建50个pvc
#sh performance/pvc.sh
2.结果：
  创建时间：20s
pvc-test-50   Bound    pvc-6c80902b-1f51-483d-9867-4fe4e6f94a0f   1Gi        RWO            csi-sc-arstorplugin   20s
  删除时间： 8s
# time kubectl delete -f /root/pfy/performance-pvc/.
persistentvolumeclaim "pvc-test-50" deleted

real    0m8.424s
user    0m0.173s
sys     0m0.069s


二、测试100个pvc
1.创建，修改performance/pvc.sh个数为100再执行
#sh performance/pvc.sh
2.结果：
  创建时间：40s
pvc-test-99    Bound    pvc-9e288896-57a2-46c5-844f-6c55be22d777   1Gi        RWO            csi-sc-arstorplugin   40s

  删除时间：19s
# time kubectl delete -f /root/pfy/performance-pvc/.
persistentvolumeclaim "pvc-test-99" deleted

real	0m18.435s
user	0m0.217s
sys	0m0.054s


总结： arstor volume的创建删除正常


三、测试30个statefulset

1.创建
#sh performance/statefulset-arstor.sh
2.结果：

pvc创建时间13s
www-statefulset-arstor-30-0   Bound    pvc-513216f4-d940-4792-a318-3a6a438e4582   1Gi        RWO            csi-sc-arstorplugin   13s

pod创建时间2m4s
statefulset-arstor-30-0   1/1     Running   0          2m4s

4.删除pod

# kubectl delete -f /root/pfy/performance-statefulset/.

删除时间：34s
statefulset-arstor-2-0    0/1     Terminating   0          4m34s  (4开始删除)


