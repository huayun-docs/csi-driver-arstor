/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package arstor

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"k8s.io/klog"
	"os"
	"reflect"
	"syscall"
	"time"

	"bytes"
	"github.com/golang/protobuf/ptypes/timestamp"
	"k8s.io/utils/mount"
	"path/filepath"
	"regexp"
)

const (
	kib    int64 = 1024
	mib    int64 = kib * 1024
	gib    int64 = mib * 1024
	gib100 int64 = gib * 100
	tib    int64 = gib * 1024
	tib100 int64 = tib * 100
)

const (
	annotationKeySnapshotArStorId           = "arstor.snapshot.kubernetes.io/id"
	annotationKeySnapshotArStorPath         = "arstor.snapshot.kubernetes.io/path"
	annotationKeySnapshotArStorSize         = "arstor.snapshot.kubernetes.io/size"
	annotationKeySnapshotArStorVolumeId     = "arstor.snapshot.kubernetes.io/volumeid"
	annotationKeySnapshotArStorCreationTime = "arstor.snapshot.kubernetes.io/creationtime"
	annotationKeySnapshotArStorReadyToUse   = "arstor.snapshot.kubernetes.io/readytouse"
)

//var (
//	keyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc
//)

func Contains(obj interface{}, target interface{}) (bool, error) {
	targetValue := reflect.ValueOf(target)
	switch reflect.TypeOf(target).Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < targetValue.Len(); i++ {
			if targetValue.Index(i).Interface() == obj {
				return true, nil
			}
		}
	case reflect.Map:
		if targetValue.MapIndex(reflect.ValueOf(obj)).IsValid() {
			return true, nil
		}
	}
	return false, errors.New("not in")
}

// String hashes a string to a unique hashcode.
//
// crc32 returns a uint32, but for our use we need
// and non negative integer. Here we cast to an integer
// and invert it if the result is negative.
func StringToHash(s string) int {
	v := int(crc32.ChecksumIEEE([]byte(s)))
	if v >= 0 {
		return v
	}
	if -v >= 0 {
		return -v
	}
	// v == MinInt
	return 0
}

func PathExists(path string) (bool, error) {
	info, err := os.Stat(path)
	klog.Infof("PathExists path %s, info %v", path, info)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		for i := 0; i < 4; i++ {
			klog.Infof("check file sleep 0.5s")
			time.Sleep(500 * time.Millisecond)
			info, err = os.Stat(path)
			klog.Infof("check again: PathExists path %s, info %v", path, info)
			if err == nil {
				return true, nil
			}
		}
		return false, nil
	}
	return false, err
}

func EnsureDir(dir string) error {

	err := os.MkdirAll(dir, os.FileMode(0750))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func Ensurefile(file string) error {
	f, err := os.OpenFile(file, os.O_CREATE, os.FileMode(0644))
	defer f.Close()
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func DeleteDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil
}

func DeleteFile(file string) error {
	if err := os.Remove(file); err != nil {
		return err
	}
	return nil
}

func GetFileDir(file string) (string, error) {
	path := filepath.Dir(file)

	dir, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return dir, nil
}

func GetPathInode(path string) (uint64, error) {
	if len(path) == 0 {
		return 0, errors.New("dir is empty")
	}

	fileinfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		message := fmt.Sprintf("Not a syscall.Stat_t")
		return 0, errors.New(message)
	}

	inode := stat.Ino
	return inode, nil
}

func GetFileSize(file string) (int64, error) {
	if len(file) == 0 {
		return 0, errors.New("file is empty")
	}

	fileinfo, err := os.Stat(file)
	if err != nil {
		return 0, err
	}

	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		message := fmt.Sprintf("Not a syscall.Stat_t")
		return 0, errors.New(message)
	}

	return stat.Size, nil
}

func WriteFile(file string, buf bytes.Buffer) error {

	err := ioutil.WriteFile(file, buf.Bytes(), 0666)
	if err != nil {
		return err
	}
	return nil
}

func GetFileCreationTime(file string) (*timestamp.Timestamp, error) {

	if len(file) == 0 {
		return nil, errors.New("file is empty")
	}

	fileinfo, err := os.Stat(file)
	if err != nil {
		return nil, err
	}

	stat, ok := fileinfo.Sys().(*syscall.Stat_t)
	if !ok {
		message := fmt.Sprintf("Not a syscall.Stat_t")
		return nil, errors.New(message)
	}

	time := timestamp.Timestamp{
		Seconds: stat.Ctim.Sec,
		Nanos:   int32(stat.Ctim.Nsec),
	}
	return &time, nil
}

func IsFileFullErr(err error) bool {
	// u'7' == re.findall('\d+', err.stderr)[0]
	str := err.Error()
	reg := regexp.MustCompile("[0-9]+")
	result := reg.Find([]byte(str))

	if string(result) == "7" {
		return true
	}

	return false
}

// storeObjectUpdate updates given cache with a new object version from Informer
// callback (i.e. with events from etcd) or with an object modified by the
// controller itself. Returns "true", if the cache was updated, false if the
// object is an old version and should be ignored.
//func storeObjectUpdate(store cache.Store, obj interface{}, className string) (bool, error) {
//	objName, err := keyFunc(obj)
//	if err != nil {
//		return false, fmt.Errorf("Couldn't get key for object %+v: %v", obj, err)
//	}
//	oldObj, found, err := store.Get(obj)
//	if err != nil {
//		return false, fmt.Errorf("Error finding %s %q in controller cache: %v", className, objName, err)
//	}
//
//	objAccessor, err := meta.Accessor(obj)
//	if err != nil {
//		return false, err
//	}
//
//	if !found {
//		// This is a new object
//		klog.V(4).Infof("storeObjectUpdate: adding %s %q, version %s", className, objName, objAccessor.GetResourceVersion())
//		if err = store.Add(obj); err != nil {
//			return false, fmt.Errorf("error adding %s %q to controller cache: %v", className, objName, err)
//		}
//		return true, nil
//	}
//
//	oldObjAccessor, err := meta.Accessor(oldObj)
//	if err != nil {
//		return false, err
//	}
//
//	objResourceVersion, err := strconv.ParseInt(objAccessor.GetResourceVersion(), 10, 64)
//	if err != nil {
//		return false, fmt.Errorf("error parsing ResourceVersion %q of %s %q: %s", objAccessor.GetResourceVersion(), className, objName, err)
//	}
//	oldObjResourceVersion, err := strconv.ParseInt(oldObjAccessor.GetResourceVersion(), 10, 64)
//	if err != nil {
//		return false, fmt.Errorf("error parsing old ResourceVersion %q of %s %q: %s", oldObjAccessor.GetResourceVersion(), className, objName, err)
//	}
//
//	// Throw away only older version, let the same version pass - we do want to
//	// get periodic sync events.
//	if oldObjResourceVersion > objResourceVersion {
//		klog.V(4).Infof("storeObjectUpdate: ignoring %s %q version %s", className, objName, objAccessor.GetResourceVersion())
//		return false, nil
//	}
//
//	klog.V(4).Infof("storeObjectUpdate updating %s %q with version %s", className, objName, objAccessor.GetResourceVersion())
//	if err = store.Update(obj); err != nil {
//		return false, fmt.Errorf("error updating %s %q in controller cache: %v", className, objName, err)
//	}
//	return true, nil
//}

//
//func GetSnapshotArStorInfo(snapshot *crdv1.VolumeSnapshot) (*arstorSnapshot, bool) {
//
//	id, ok := snapshot.ObjectMeta.Annotations[annotationKeySnapshotArStorId]
//	if !ok {
//		return nil, false
//	}
//
//	path, ok := snapshot.ObjectMeta.Annotations[annotationKeySnapshotArStorPath]
//	if !ok {
//		return nil, false
//	}
//
//	volumeId, ok := snapshot.ObjectMeta.Annotations[annotationKeySnapshotArStorVolumeId]
//	if !ok {
//		return nil, false
//	}
//
//	creationTime := ptypes.TimestampNow()
//	_, ok = snapshot.ObjectMeta.Annotations[annotationKeySnapshotArStorCreationTime]
//	if !ok {
//		return nil, false
//	}
//
//	var readyToUse bool
//	str, ok := snapshot.ObjectMeta.Annotations[annotationKeySnapshotArStorReadyToUse]
//	if !ok {
//		readyToUse, _ = strconv.ParseBool(str)
//		return nil, true
//	}
//
//	return &arstorSnapshot{
//		Id:           id,
//		Path:         path,
//		Size:         1,
//		VolumeId:     volumeId,
//		CreationTime: *creationTime,
//		ReadyToUse:   readyToUse,
//	}, false
//}

func GetVolume(volumes map[string]arstorVolume, unique string) *arstorVolume {
	volume := GetVolumeById(volumes, unique)
	if volume != nil {
		return volume
	}

	volume = GetVolumeByName(volumes, unique)
	if volume != nil {
		return volume
	}

	return nil
}

func GetVolumeByName(volumes map[string]arstorVolume, name string) *arstorVolume {
	for _, volume := range volumes {
		if volume.Name == name {
			return &volume
		}
	}

	return nil
}

func GetVolumeById(volumes map[string]arstorVolume, id string) *arstorVolume {
	volume, ok := volumes[id]
	if !ok {
		return nil
	}

	return &volume
}

func GetSnapshot(snapshots map[string]arstorSnapshot, unique string) *arstorSnapshot {
	snapshot := GetSnapshotById(snapshots, unique)
	if snapshot != nil {
		return snapshot
	}

	snapshot = GetSnapshotByName(snapshots, unique)
	if snapshot != nil {
		return snapshot
	}

	return nil
}

func GetSnapshotByName(snapshots map[string]arstorSnapshot, name string) *arstorSnapshot {
	for _, snapshot := range snapshots {
		if snapshot.Name == name {
			return &snapshot
		}
	}

	return nil
}

func GetSnapshotById(snapshots map[string]arstorSnapshot, id string) *arstorSnapshot {
	snapshot, ok := snapshots[id]
	if !ok {
		return nil
	}

	return &snapshot
}

func GetAllFile(basePath string, dirPath string, files map[string]string) error {
	rd, err := ioutil.ReadDir(basePath + dirPath)
	if err != nil {
		klog.Infof("read dir %s fail: %v", basePath+dirPath, err)
		return err
	}

	for _, fi := range rd {
		if fi.IsDir() {
			fullDir := dirPath + "/" + fi.Name()
			err = GetAllFile(basePath, fullDir, files)
			if err != nil {
				klog.Infof("read dir %s fail: %v", basePath+fullDir, err)
				return err
			}
		} else {
			fullPath := dirPath + "/" + fi.Name()
			files[fi.Name()] = fullPath
		}
	}
	return nil
}

func IsLikelyNotMountPointAttach(targetpath string) (bool, error) {
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetpath)
	klog.Infof("IsLikelyNotMountPointAttach %s err: %v", targetpath, err)
	if err != nil && !os.IsNotExist(err) {
		klog.Errorf("cannot validate mountpoint: %s", targetpath)
		return notMnt, err
	}
	if !notMnt && err == nil {
		return false, nil
	}

	if err := os.MkdirAll(targetpath, 0750); err != nil {
		klog.Errorf("failed to mkdir:%s", targetpath)
		return notMnt, err
	}
	klog.Infof("IsLikelyNotMountPointAttach mkdir %s ", targetpath)

	return true, nil
}

func IsLikelyNotMountPointDetach(targetpath string) (bool, error) {

	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetpath)
	if err != nil && !os.IsNotExist(err) {
		return notMnt, err
	}

	if os.IsNotExist(err) {
		return true, nil
	}

	return notMnt, nil
}

// UnmountPath
func UnmountPath(mountPath string) error {
	klog.Infof("UnmountPath %s", mountPath)
	return mount.CleanupMountPoint(mountPath, mount.New(""), false /* extensiveMountPointCheck */)
}

// RoundUpSize calculates how many allocation units are needed to accommodate
// a volume of given size. E.g. when user wants 1500MiB volume, while AWS EBS
// allocates volumes in gibibyte-sized chunks,
// RoundUpSize(1500 * 1024*1024, 1024*1024*1024) returns '2'
// (2 GiB is the smallest allocatable volume that can hold 1500MiB)
func RoundUpSize(volumeSizeBytes int64, allocationUnitBytes int64) int64 {
	roundedUp := volumeSizeBytes / allocationUnitBytes
	if volumeSizeBytes%allocationUnitBytes > 0 {
		roundedUp++
	}
	return roundedUp
}
