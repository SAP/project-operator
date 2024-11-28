# Manage Custom Namespaces in Kubernetes

[![REUSE status](https://api.reuse.software/badge/github.com/SAP/project-operator)](https://api.reuse.software/info/github.com/SAP/project-operator)

## About this project

The goal of this project is to enable individuals, teams, or pipelines, to setup (certain) Kubernetes namespaces including RBAC configuration, without
having or needing authorization to manage the underlying namespace or RBAC entities directly.
This is achieved by adding a custom resource type `projects.core.cs.sap.com` to Kubernetes clusters, which could be instantiated like this:

```yaml
apiVersion: core.cs.sap.com/v1alpha1
kind: Project
metadata:
  name: awesome-stuff
spec:
  labels:
    properties.domain.com/awesome: "true"
  annotations:
    properties.domain.com/cool: "forsure"
  adminUsers:
  - someoneimportant@domain.com
  - masterbrain@otherdomain.com
  adminGroups:
  - peoplewhoknowwhattheyaredoing
  viewerUsers:
  - somebodyelse@domain.com
  viewerGroups:
  - normalpeople
  ```

The idea is now to authorize users (such as teams or pipelines) to manage these `Project` resources, instead of giving them rights on namespaces or RBAC entities.
When reconciling such resources, the operator contained in this repository creates a namespace named like the project,  prefixed by `project-`, such as `project-awesome-stuff` in the above example, and maintains role bindings in that namespace, granting admin/view permissions to
the identities defined in the project's spec. By default the admin rolebinding will reference the built-in `cluster-admin` cluster role, and the viewer rolebinding will reference the built-in `view` cluster role,
but this can be overridden by the following command line flags:

```bash
  -admin-cluster-role string
      Cluster role that admin users/groups will be granted on project namespace level.
      (default "cluster-admin")
  -viewer-cluster-role string
      Cluster role that viewer users/groups will be granted on project namespace level.
      (default "view")
```

In addition, the operator can be instructed to grant cluster view permissions (i.e. create a cluster role binding to the `view` cluster role) to all identities occurring in the project's spec:

```bash
  -enable-cluster-view
      Automatically grant cluster view authorizations to all referenced users/groups.
```
The prefix used to construct the namespace name from the project name (default: `project-`) can be
overridden by command line flag:

```bash
  -namespace-prefix string
      Prefix of generated namespaces. (default "project-")
```

Note that setting this prefix to the empty string is forbidden due to security reasons.

When updating or deleting a project resource, the operator applies additional authorization logic
(besides the normal RBAC logic):
- no additional authorization checks are enforced for service accounts
- no additional authorization checks are enforced for users/groups listed in `spec.adminUsers` or `spec.adminGroups`
- other users will be denied unless they have the authorization to perform the analogous operation (update or delete) on the namespace managed by the project.

## Requirements and Setup

The recommended deployment method is to use the [Helm chart](https://github.com/sap/project-operator-helm):

```bash
helm upgrade -i project-operator oci://ghcr.io/sap/project-operator-helm/project-operator
```

## Documentation
 
The API reference is here: [https://pkg.go.dev/github.com/sap/project-operator](https://pkg.go.dev/github.com/sap/project-operator).

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/SAP/project-operator/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2024 SAP SE or an SAP affiliate company and project-operator contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/SAP/project-operator).
