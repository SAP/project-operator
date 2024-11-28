/*
SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ProjectLister helps list Projects.
// All objects returned here must be treated as read-only.
type ProjectLister interface {
	// List lists all Projects in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Project, err error)
	// Projects returns an object that can list and get Projects.
	Projects(namespace string) ProjectNamespaceLister
	ProjectListerExpansion
}

// projectLister implements the ProjectLister interface.
type projectLister struct {
	indexer cache.Indexer
}

// NewProjectLister returns a new ProjectLister.
func NewProjectLister(indexer cache.Indexer) ProjectLister {
	return &projectLister{indexer: indexer}
}

// List lists all Projects in the indexer.
func (s *projectLister) List(selector labels.Selector) (ret []*v1alpha1.Project, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Project))
	})
	return ret, err
}

// Projects returns an object that can list and get Projects.
func (s *projectLister) Projects(namespace string) ProjectNamespaceLister {
	return projectNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ProjectNamespaceLister helps list and get Projects.
// All objects returned here must be treated as read-only.
type ProjectNamespaceLister interface {
	// List lists all Projects in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Project, err error)
	// Get retrieves the Project from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.Project, error)
	ProjectNamespaceListerExpansion
}

// projectNamespaceLister implements the ProjectNamespaceLister
// interface.
type projectNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Projects in the indexer for a given namespace.
func (s projectNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.Project, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Project))
	})
	return ret, err
}

// Get retrieves the Project from the indexer for a given namespace and name.
func (s projectNamespaceLister) Get(name string) (*v1alpha1.Project, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("project"), name)
	}
	return obj.(*v1alpha1.Project), nil
}
