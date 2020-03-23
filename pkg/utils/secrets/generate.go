// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package secrets

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	utilerrors "github.com/gardener/gardener/pkg/utils/errors"
)

// GenerateAndDeployClusterSecrets tries to generate and deploy each secret in the wantedSecretsList to the k8s cluster.
// If the secret already exist it jumps to the next one. The function returns a map with all of the successfully deployed
// wanted secrets plus those already deployed (only from the wantedSecretsList).
func GenerateAndDeployClusterSecrets(ctx context.Context, k8sClusterClient kubernetes.Interface, existingSecretsMap map[string]*corev1.Secret, wantedSecretsList []ConfigInterface, namespace string) (map[string]*corev1.Secret, error) {
	errorList := &multierror.Error{
		ErrorFormat: utilerrors.NewErrorFormatFuncWithPrefix("cluster secrets generation and deployment"),
	}

	clusterSecrets, generateErrors := GenerateClusterSecrets(existingSecretsMap, wantedSecretsList, namespace)
	errorList = multierror.Append(errorList, generateErrors)

	var (
		wg      sync.WaitGroup
		errChan = make(chan error)
	)

	for name, secret := range clusterSecrets {
		if _, exists := existingSecretsMap[name]; exists {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := k8sClusterClient.Client().Create(ctx, secret); err != nil {
				errChan <- fmt.Errorf("error deploying secret '%s/%s': %+v", namespace, name, err)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for err := range errChan {
		errorList = multierror.Append(err)
	}

	return clusterSecrets, errorList.ErrorOrNil()
}

// GenerateClusterSecrets generates all secrets in the wantedSecretsList. If the secret already exists it takes the existing
// secret instead of generating a new one. The function returns a map with all of the newly generated secrets plus those
// already existing (only from the wantedSecretsList).
func GenerateClusterSecrets(existingSecretsMap map[string]*corev1.Secret, wantedSecretsList []ConfigInterface, namespace string) (map[string]*corev1.Secret, error) {
	type secretOutput struct {
		secret *corev1.Secret
		err    error
	}

	var (
		results        = make(chan *secretOutput)
		clusterSecrets = map[string]*corev1.Secret{}
		wg             sync.WaitGroup
		errorList      = &multierror.Error{
			ErrorFormat: utilerrors.NewErrorFormatFuncWithPrefix("cluster secrets generation"),
		}
	)

	for _, s := range wantedSecretsList {
		name := s.GetName()

		if existingSecret, ok := existingSecretsMap[name]; ok {
			clusterSecrets[name] = existingSecret
			continue
		}

		wg.Add(1)
		go func(s ConfigInterface) {
			defer wg.Done()

			obj, err := s.Generate()
			if err != nil {
				results <- &secretOutput{err: errors.WithMessagef(err, "error generating secret '%s/%s'", namespace, name)}
				return
			}

			secretType := corev1.SecretTypeOpaque
			if _, isTLSSecret := obj.(*Certificate); isTLSSecret {
				secretType = corev1.SecretTypeTLS
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s.GetName(),
					Namespace: namespace,
				},
				Type: secretType,
				Data: obj.SecretData(),
			}
			results <- &secretOutput{secret: secret}
		}(s)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for out := range results {
		if out.err != nil {
			errorList = multierror.Append(errorList, out.err)
			continue
		}

		clusterSecrets[out.secret.Name] = out.secret
	}

	return clusterSecrets, errorList.ErrorOrNil()
}
