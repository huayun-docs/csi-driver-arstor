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

package tool

import (
	"errors"
	"os"
	"reflect"
)

const (
	kib    int64 = 1024
	mib    int64 = kib * 1024
	gib    int64 = mib * 1024
	gib100 int64 = gib * 100
	tib    int64 = gib * 1024
	tib100 int64 = tib * 100
)

var PageSize = [...]string{"4096", "8192", "16384", "32768"}
var Readcache = [...]string{"True", "False", "true", "false"}
var Mirroring = [...]string{"2", "3"}

var CompressionMap = map[string]string{
	"lz4":       "COMPRESSION_ALGORITHM_LZ4",
	"gzip_opt":  "COMPRESSION_ALGORITHM_GZIP_OPT",
	"gzip_high": "COMPRESSION_ALGORITHM_GZIP_HIGH",
	"disabled":  "COMPRESSION_ALGORITHM_OFF",
}

var AttributionMap = map[string]string{
	"Policy.LocalFS.pageSize":                  "pagesize",
	"Policy.LocalFS.compression":               "compression",
	"Policy.LocalFS.readCache":                 "read_cache",
	"Policy.LocalFS.compressionAlgorithm":      "compression_algorithm",
	"Policy.Policy.Mirroring.numberOfMirrors":  "mirroring",
	"Policy.Policy.Mirroring.readStripeSize":   "read_stripe_size",
	"Policy.Policy.Striping.stripeWidth":       "stripe_width",
	"Policy.Policy.Striping.stripeSize":        "stripe_size",
	"Policy.Policy.Striping.stripeAcrossNodes": "stripe_across_nodes",
	"Policy.Policy.rebuildPriority":            "rebuild_priority",
	"Policy.Policy.faultDomainWidth":           "fault_domain_width",
	"Policy.Policy.inherit":                    "inherit",
	"Policy.Policy.metroCluster":               "metro_cluster",
	"Policy.Policy.goldenImage":                "golden_image",
	"Policy.Policy.placementRules":             "placement_rules",
}

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

func DeleteFile(file string) error {
	if err := os.Remove(file); err != nil {
		return err
	}
	return nil
}
