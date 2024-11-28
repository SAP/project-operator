/*
SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package deployer

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type Deployer struct {
	client              client.Client
	scheme              *runtime.Scheme
	managedGVKs         []schema.GroupVersionKind
	labelKeyOwner       string
	annotationKeyDigest string
}

// note: specified client must not cache unstructured objects
func New(client client.Client, scheme *runtime.Scheme, managedGVKS []schema.GroupVersionKind, labelKeyOwner string, annotationKeyDigest string) *Deployer {
	return &Deployer{
		client:              client,
		scheme:              scheme,
		managedGVKs:         managedGVKS,
		labelKeyOwner:       labelKeyOwner,
		annotationKeyDigest: annotationKeyDigest,
	}
}

type worklistItem struct {
	old *unstructured.Unstructured
	new *unstructured.Unstructured
}

func (d *Deployer) ReconcileObjects(ctx context.Context, objects []client.Object, owner string) (bool, error) {
	restMappings := make(map[schema.GroupVersionKind]*meta.RESTMapping)
	worklist := make(map[string]*worklistItem)
	selector := labels.SelectorFromSet(map[string]string{d.labelKeyOwner: owner})

	for _, gvk := range d.managedGVKs {
		restMapping, err := d.client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return false, err
		}
		restMappings[gvk] = restMapping

		objectList := &unstructured.UnstructuredList{}
		objectList.SetGroupVersionKind(gvk)
		if err := d.client.List(ctx, objectList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
			return false, err
		}
		for i := 0; i < len(objectList.Items); i++ {
			worklist[objectKey(&objectList.Items[i])] = &worklistItem{old: &objectList.Items[i]}
		}
	}

	for _, object := range objects {
		gvk, err := apiutil.GVKForObject(object, d.scheme)
		if err != nil {
			return false, err
		}
		restMapping, ok := restMappings[gvk]
		if !ok {
			return false, fmt.Errorf("type %s is not managed", gvk)
		}

		switch restMapping.Scope.Name() {
		case meta.RESTScopeNameRoot:
			if object.GetNamespace() != "" {
				return false, fmt.Errorf("cluster-scope object %s %s must not have namespace set", gvk, object.GetName())
			}
		case meta.RESTScopeNameNamespace:
			if object.GetNamespace() == "" {
				return false, fmt.Errorf("namespace-scope object %s %s requires namespace", gvk, object.GetName())
			}
		default:
			panic("this cannot happen")
		}

		_gvk := object.GetObjectKind().GroupVersionKind()
		if _gvk.Empty() {
			object.GetObjectKind().SetGroupVersionKind(gvk)
		} else if _gvk != gvk {
			return false, fmt.Errorf("inconsistent GroupVersionKind for object %s %s", _gvk, objectName(object))
		}

		unstructured, err := toUnstructured(object)
		if err != nil {
			return false, err
		}

		digest, err := calculateDigest(object)
		if err != nil {
			return false, err
		}
		setLabel(unstructured, d.labelKeyOwner, owner)
		setAnnotation(unstructured, d.annotationKeyDigest, digest)

		if item, ok := worklist[objectKey(unstructured)]; ok {
			item.new = unstructured
		} else {
			worklist[objectKey(unstructured)] = &worklistItem{new: unstructured}
		}
	}

	numUnready := 0
	for _, item := range worklist {
		if item.old == nil {
			// note: err might be NotFound if namespace does not yet exist
			// in that case we suppress the error, but increase unready to try again later
			if err := d.client.Create(ctx, item.new); client.IgnoreNotFound(err) != nil {
				return false, err
			}
			numUnready++
		} else if item.new == nil {
			preconditions := metav1.Preconditions{ResourceVersion: &[]string{item.old.GetResourceVersion()}[0]}
			// note: err might be NotFound if object is already deleted, e.g. manually or by garbage collection
			if err := d.client.Delete(ctx, item.old, client.Preconditions(preconditions)); client.IgnoreNotFound(err) != nil {
				return false, err
			}
			numUnready++
		} else if item.new.GetAnnotations()[d.annotationKeyDigest] != item.old.GetAnnotations()[d.annotationKeyDigest] {
			// TODO: make it more configurable which objects require recreation;
			// for example, by a dark annotation in the specified objects ...
			if item.new.GetKind() == "RoleBinding" {
				preconditions := metav1.Preconditions{ResourceVersion: &[]string{item.old.GetResourceVersion()}[0]}
				// note: err might be NotFound if object is already deleted, e.g. manually or by garbage collection
				if err := d.client.Delete(ctx, item.old, client.Preconditions(preconditions)); client.IgnoreNotFound(err) != nil {
					return false, err
				}
			} else {
				item.new.SetResourceVersion(item.old.GetResourceVersion())
				if err := d.client.Update(ctx, item.new); err != nil {
					return false, err
				}
			}
			numUnready++
		} else {
			res, err := kstatus.Compute(item.old)
			if err != nil {
				return false, err
			}
			if res.Status != kstatus.CurrentStatus {
				numUnready++
			}
		}
	}

	return numUnready == 0, nil
}
