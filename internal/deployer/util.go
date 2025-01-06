/*
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package deployer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func objectName(object client.Object) string {
	namespace := object.GetNamespace()
	name := object.GetName()
	if namespace == "" {
		return name
	} else {
		return namespace + "/" + name
	}
}

func objectKey(object client.Object) string {
	gvk := object.GetObjectKind().GroupVersionKind()
	return fmt.Sprintf("%s %s", gvk.GroupKind(), objectName(object))
}

func toUnstructured(object client.Object) (*unstructured.Unstructured, error) {
	unstructuredContent, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: unstructuredContent}, nil
}

func setLabel(object metav1.Object, key string, value string) {
	labels := object.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[key] = value
	object.SetLabels(labels)
}

func setAnnotation(object metav1.Object, key string, value string) {
	annotations := object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	object.SetAnnotations(annotations)
}

func calculateDigest(object client.Object) (string, error) {
	raw, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
