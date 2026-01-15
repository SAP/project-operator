/*
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func mustGVKForObject(object runtime.Object, scheme *runtime.Scheme) schema.GroupVersionKind {
	gvk, err := apiutil.GVKForObject(object, scheme)
	if err != nil {
		panic(err)
	}
	return gvk
}
