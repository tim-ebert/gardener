// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package healthz

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

type cacheSyncWaiter interface {
	WaitForCacheSync(ctx context.Context) bool
}

// NewCacheSyncHealthz returns a new healthz.Checker that will pass only if all informers in the given cacheSyncWaiter sync.
func NewCacheSyncHealthz(cacheSyncWaiter cacheSyncWaiter) healthz.Checker {
	return func(_ *http.Request) error {
		// cache.Cache.WaitForCacheSync is racy for closed context, so use context with 1ms timeout instead.
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		if !cacheSyncWaiter.WaitForCacheSync(ctx) {
			return fmt.Errorf("informers not synced yet")
		}
		return nil
	}
}

// NewInformerSyncHealthz returns a new healthz.Checker that will pass only if all informers for the given object kinds
// in the informersCache sync.
func NewInformerSyncHealthz(informersCache cache.Informers, scheme *runtime.Scheme, objs ...runtime.Object) (healthz.Checker, error) {
	if len(objs) == 0 {
		return nil, fmt.Errorf("no objects provided")
	}

	var gvks []schema.GroupVersionKind
	for _, obj := range objs {
		gvk, err := apiutil.GVKForObject(obj, scheme)
		if err != nil {
			return nil, err
		}
		gvks = append(gvks, gvk)
	}

	return func(_ *http.Request) error {
		// cancel context to force checking if informers are synced now.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		informersBySynced := make(map[bool][]string)
		for _, gvk := range gvks {
			synced := false
			informer, err := informersCache.GetInformerForKind(ctx, gvk)
			if err == nil {
				synced = informer.HasSynced()
			}
			informersBySynced[synced] = append(informersBySynced[synced], gvk.String())
		}

		if notSynced := informersBySynced[false]; len(notSynced) > 0 {
			return fmt.Errorf("%d informers not synced yet: %v", len(notSynced), notSynced)
		}
		return nil
	}, nil
}
