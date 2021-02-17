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

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/logger"
	"github.com/gardener/gardener/pkg/operation/common"
	"github.com/gardener/gardener/pkg/utils"
)

const (
	namespaceValidatorName = "namespace_validator"
)

var namespaceGVK = metav1.GroupVersionKind{Group: "", Kind: "Namespace", Version: "v1"}

type namespaceDeletionHandler struct {
	cacheReader client.Reader
	apiReader   client.Reader

	decoder *admission.Decoder
	logger  logr.Logger
}

// NewValidateNamespaceDeletionHandler creates a new handler for validating namespace deletions.
func NewValidateNamespaceDeletionHandler(ctx context.Context, cache cache.Cache) (admission.Handler, error) {
	// Initialize caches here to ensure the readyz informer check will only succeed once informers required for this
	// handler have synced so that http requests can be served quicker with pre-syncronized caches.
	_, err := cache.GetInformer(ctx, &gardencorev1beta1.Project{})
	if err != nil {
		return nil, err
	}

	return &namespaceDeletionHandler{
		cacheReader: cache,
	}, nil
}

// InjectLogger injects a logger into the handler.
func (h *namespaceDeletionHandler) InjectLogger(l logr.Logger) error {
	h.logger = l.WithName(namespaceValidatorName)
	return nil
}

// InjectAPIReader injects a reader into the handler.
func (h *namespaceDeletionHandler) InjectAPIReader(reader client.Reader) error {
	h.apiReader = reader
	return nil
}

// InjectDecoder injects a decoder capable of decoding objects included in admission requests.
func (h *namespaceDeletionHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

// Handle implements the webhook handler for namespace deletion validation.
func (h *namespaceDeletionHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// If the request does not indicate the correct operation (DELETE) we allow the review without further doing.
	if request.Operation != admissionv1.Delete {
		return admissionAllowed("operation is not DELETE")
	}
	if request.Kind != namespaceGVK {
		return admissionAllowed("resource is not corev1.Namespace")
	}
	if request.SubResource != "" {
		return admissionAllowed("subresources on secrets are not handled")
	}

	requestID, err := utils.GenerateRandomString(8)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	requestLogger := h.logger.WithValues(logger.IDFieldName, requestID)

	// Now that all checks have been passed we can actually validate the admission request.
	reviewResponse := h.admitNamespace(ctx, request)
	if !reviewResponse.Allowed && reviewResponse.Result != nil {
		requestLogger.Info("rejected namespace deletion", "user", request.UserInfo.Username, "message", reviewResponse.Result.Message)
	}
	return reviewResponse
}

// admitNamespace does only allow the request if no Shoots exist in this specific namespace anymore.
func (h *namespaceDeletionHandler) admitNamespace(ctx context.Context, request admission.Request) admission.Response {
	// Determine project for given namespace.
	// TODO: we should use a direct lookup here, as we might falsely allow the request, if our cache is
	// out of sync and doesn't know about the project. We should use a field selector for looking up the project
	// belonging to a given namespace.
	project, err := common.ProjectForNamespaceWithClient(ctx, h.cacheReader, request.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return admissionAllowed("namespace does not belong to a project")
		}
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// if we do not receive the namespace object in the `.object` field of the admission request, we need to get it ourselves.
	namespace := &corev1.Namespace{}
	if err := h.apiReader.Get(ctx, client.ObjectKey{Name: request.Name}, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return admissionAllowed("namespace is already gone")
		}
		return admission.Errored(http.StatusInternalServerError, err)
	}

	switch {
	case namespace.DeletionTimestamp != nil:
		return admissionAllowed("namespace is already marked for deletion")
	case project.DeletionTimestamp != nil:
		// if project is marked for deletion we need to wait until all shoots in the namespace are gone
		namespaceEmpty, err := h.isNamespaceEmpty(ctx, namespace.Name)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if namespaceEmpty {
			return admissionAllowed("namespace doesn't contain any shoots")
		}

		return admission.Errored(http.StatusUnprocessableEntity, fmt.Errorf("deletion of namespace %q is not permitted (it still contains Shoots)", namespace.Name))
	}

	// Namespace is not yet marked for deletion and project is not marked as well. We do not admit and respond that
	// namespace deletion is only allowed via project deletion.
	return admission.Errored(http.StatusUnprocessableEntity, fmt.Errorf("direct deletion of namespace %q is not permitted (you must delete the corresponding project %q)", namespace.Name, project.Name))
}

// isNamespaceEmpty checks if there are no more Shoots left inside the given namespace.
func (h *namespaceDeletionHandler) isNamespaceEmpty(ctx context.Context, namespace string) (bool, error) {
	shoots := &metav1.PartialObjectMetadataList{}
	shoots.SetGroupVersionKind(gardencorev1beta1.SchemeGroupVersion.WithKind("Shoot"))
	if err := h.apiReader.List(ctx, shoots, client.InNamespace(namespace)); err != nil {
		return false, err
	}

	return len(shoots.Items) == 0, nil
}
