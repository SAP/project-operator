/*
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and project-operator contributors
SPDX-License-Identifier: Apache-2.0
*/

package webhooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/sap/go-generics/slices"

	admissionv1 "k8s.io/api/admission/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/sap/project-operator/api/v1alpha1"
	"github.com/sap/project-operator/controllers"
)

// log is for logging in this package.
var projectlog = logf.Log.WithName("project-resource")

// +kubebuilder:webhook:path=/validate-core-cs-sap-com-v1alpha1-project,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.cs.sap.com,resources=projects,verbs=create;update;delete,versions=v1alpha1,name=vproject.kb.io,admissionReviewVersions=v1

// ProjectWebhookOptions defines options for a ProjectWebhook
type ProjectReconcilerOptions struct {
	NamespacePrefix string
}

// ProjectWebhook provides admission logic for Project objects
type ProjectWebhook struct {
	client  client.Client
	options ProjectReconcilerOptions
}

func NewProjectWebhook(client client.Client, options ProjectReconcilerOptions) *ProjectWebhook {
	return &ProjectWebhook{
		client: client,
		// TODO: whenever options will contain deep fields or pointers, we should deep-clone it here
		options: options,
	}
}

func (w *ProjectWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// note: it should be safe to cast obj to *Project
	project := obj.(*corev1alpha1.Project)
	projectlog.Info("validate create", "name", project.Name)

	if err := w.validate(ctx, project); err != nil {
		return nil, err
	}

	return nil, nil
}

func (w *ProjectWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	// note: it should be safe to cast oldObj, newObj to *Project
	oldProject := oldObj.(*corev1alpha1.Project)
	newProject := newObj.(*corev1alpha1.Project)
	projectlog.Info("validate update", "name", newProject.Name)

	if err := w.validate(ctx, newProject); err != nil {
		return nil, err
	}

	return nil, w.authorize(ctx, oldProject)
}

func (w *ProjectWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// note: it should be safe to cast obj to *Project
	project := obj.(*corev1alpha1.Project)
	projectlog.Info("validate delete", "name", project.Name)

	return nil, w.authorize(ctx, project)
}

func (w *ProjectWebhook) validate(ctx context.Context, project *corev1alpha1.Project) error {
	if value, ok := project.Annotations[controllers.AnnotationKeyTTL]; ok {
		_, err := time.ParseDuration(value)
		if err != nil {
			return errors.Wrapf(err, "invalid value for annotation %s: %s", controllers.AnnotationKeyTTL, value)
		}
	}
	return nil
}

func (w *ProjectWebhook) authorize(ctx context.Context, project *corev1alpha1.Project) error {
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewInternalError(errors.Wrap(err, "error extracting admission review request from context"))
	}

	userinfo := req.UserInfo
	operation := req.Operation
	verb := ""
	action := ""
	switch operation {
	case admissionv1.Create, admissionv1.Connect:
		// no additional authorization enforced on create/connect operations
		return nil
	case admissionv1.Update:
		verb = "PUT"
		action = "update"
	case admissionv1.Delete:
		verb = "DELETE"
		action = "delete"
	default:
		panic("this cannot happen")
	}

	// no additional authorization enforced for service account requests
	if strings.HasPrefix(userinfo.Username, "system:serviceaccount:") {
		return nil
	}

	// no additional authorization enforced if triggering identity has admin rights in this project
	if slices.Contains(project.Spec.AdminUsers, userinfo.Username) {
		return nil
	}
	for _, group := range userinfo.Groups {
		if slices.Contains(project.Spec.AdminGroups, group) {
			return nil
		}
	}

	// for all other cases check if the triggering identity would have permissions to update/delete the namespace related to the project
	subjectAccessReview := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			UID:    userinfo.UID,
			User:   userinfo.Username,
			Groups: userinfo.Groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Group:    "",
				Version:  "v1",
				Resource: "namespaces",
				Name:     w.options.NamespacePrefix + project.Name,
				Verb:     verb,
			},
		},
	}
	if err := w.client.Create(ctx, subjectAccessReview); err != nil {
		return apierrors.NewInternalError(errors.Wrap(err, "error creating subject access review"))
	}
	if subjectAccessReview.Status.Allowed {
		return nil
	}

	return fmt.Errorf("user %s is not allowed to %s %s.%s %s", req.UserInfo.Username, action, req.Resource.Resource, req.Resource.Group, req.Name)
}

func (w *ProjectWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1alpha1.Project{}).
		WithValidator(w).
		Complete()
}
