/*
SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	corecssapcomv1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	versioned "github.com/sap/project-operator/pkg/client/clientset/versioned"
	internalinterfaces "github.com/sap/project-operator/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/sap/project-operator/pkg/client/listers/core.cs.sap.com/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ProjectInformer provides access to a shared informer and lister for
// Projects.
type ProjectInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.ProjectLister
}

type projectInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewProjectInformer constructs a new informer for Project type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewProjectInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredProjectInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredProjectInformer constructs a new informer for Project type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredProjectInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CoreV1alpha1().Projects(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CoreV1alpha1().Projects(namespace).Watch(context.TODO(), options)
			},
		},
		&corecssapcomv1alpha1.Project{},
		resyncPeriod,
		indexers,
	)
}

func (f *projectInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredProjectInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *projectInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&corecssapcomv1alpha1.Project{}, f.defaultInformer)
}

func (f *projectInformer) Lister() v1alpha1.ProjectLister {
	return v1alpha1.NewProjectLister(f.Informer().GetIndexer())
}