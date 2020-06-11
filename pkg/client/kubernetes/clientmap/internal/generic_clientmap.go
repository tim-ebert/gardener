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

package internal

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"golang.org/x/time/rate"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
)

var _ clientmap.ClientMap = &GenericClientMap{}

const (
	waitForCacheSyncTimeout = 5 * time.Minute
	maxRefreshInterval      = 5 * time.Second
)

type GenericClientMap struct {
	clientSets map[clientmap.ClientSetKey]*clientMapEntry
	factory    clientmap.ClientSetFactory

	// mu guards concurrent access to clientSets
	mu sync.RWMutex

	log logrus.FieldLogger

	started bool
	stopCh  <-chan struct{}
}

type clientMapEntry struct {
	clientSet kubernetes.Interface
	synced    bool
	cancel    context.CancelFunc

	// refreshLimiter limits the attempts to refresh the entry due to an outdated server version.
	refreshLimiter *rate.Limiter
}

func NewGenericClientMap(factory clientmap.ClientSetFactory, logger logrus.FieldLogger) *GenericClientMap {
	return &GenericClientMap{
		clientSets: make(map[clientmap.ClientSetKey]*clientMapEntry),
		factory:    factory,
		log:        logger,
	}
}

func (cm *GenericClientMap) GetClient(ctx context.Context, key clientmap.ClientSetKey) (kubernetes.Interface, error) {
	entry, started, found := func() (*clientMapEntry, bool, bool) {
		cm.mu.RLock()
		defer cm.mu.RUnlock()

		entry, found := cm.clientSets[key]
		return entry, cm.started, found
	}()

	if found {
		if entry.refreshLimiter.Allow() {
			// invalidate client if server version has changed
			serverVersion, err := entry.clientSet.Kubernetes().Discovery().ServerVersion()
			if err != nil {
				return nil, fmt.Errorf("failed to discover server version: %w", err)
			}

			if serverVersion.GitVersion != entry.clientSet.Version() {
				if err := cm.InvalidateClient(key); err != nil {
					return nil, fmt.Errorf("error invalidating ClientSet for key %q due to changed server version: %w", key.Key(), err)
				}
				found = false
			}
		}
	}

	if !found {
		var err error
		if entry, started, err = cm.addClientSet(ctx, key); err != nil {
			return nil, err
		}
	}

	if started {
		// limit the amount of time to wait for a cache sync, as this can block controller worker routines
		// and we don't want to block all workers if it takes a long time to sync some caches
		waitContext, cancel := context.WithTimeout(ctx, waitForCacheSyncTimeout)
		defer cancel()

		// make sure the ClientSet has synced before returning
		if !cm.waitForClientSetCacheSync(entry, waitContext.Done()) {
			return nil, fmt.Errorf("timed out waiting for caches of ClientSet with key %q to sync", key.Key())
		}
	}

	return entry.clientSet, nil
}

func (cm *GenericClientMap) addClientSet(ctx context.Context, key clientmap.ClientSetKey) (*clientMapEntry, bool, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// ClientSet might have been created in the meanwhile (e.g. two goroutines might concurrently call
	// GetClient() when the ClientSet is not yet created)
	if entry, found := cm.clientSets[key]; found {
		return entry, cm.started, nil
	}

	cm.log.Infof("Creating new ClientSet for key %q", key.Key())
	cs, err := cm.factory.NewClientSet(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("error creating new ClientSet for key %q: %w", key.Key(), err)
	}

	entry := &clientMapEntry{
		clientSet:      cs,
		refreshLimiter: rate.NewLimiter(rate.Every(maxRefreshInterval), 1),
	}
	// add ClientSet to map
	cm.clientSets[key] = entry

	// if ClientMap is not started, then don't automatically start new ClientSets
	if cm.started {
		cm.startClientSet(entry)
	}

	return entry, cm.started, nil
}

func (cm *GenericClientMap) InvalidateClient(key clientmap.ClientSetKey) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	entry, found := cm.clientSets[key]
	if !found {
		return nil
	}

	cm.log.Infof("Invalidating ClientSet for key %q", key.Key())
	if entry.cancel != nil {
		entry.cancel()
	}

	delete(cm.clientSets, key)

	return nil
}

func (cm *GenericClientMap) Start(stopCh <-chan struct{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return nil
	}

	cm.stopCh = stopCh

	for _, entry := range cm.clientSets {
		cm.startClientSet(entry)
	}

	// set started to true, so we immediately start all newly created clientsets
	cm.started = true
	return nil
}

func (cm *GenericClientMap) startClientSet(entry *clientMapEntry) {
	clientSetContext, clientSetCancel := context.WithCancel(context.Background())
	go func() {
		<-cm.stopCh
		clientSetCancel()
	}()

	entry.cancel = clientSetCancel

	entry.clientSet.Start(clientSetContext.Done())
}

func (cm *GenericClientMap) waitForClientSetCacheSync(entry *clientMapEntry, waitStopCh <-chan struct{}) bool {
	// We don't need a lock here, as waiting in multiple goroutines is not harmful.
	// But RLocking here (for every GetClient) would be blocking creating of new clients, so we should avoid that.
	if entry.synced {
		return true
	}

	if !entry.clientSet.WaitForCacheSync(waitStopCh) {
		return false
	}

	entry.synced = true
	return true
}
