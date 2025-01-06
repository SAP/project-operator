#!/usr/bin/env bash

set -eo pipefail

BASEDIR=$(realpath "$(dirname "$0")"/..)

export GOBIN=$BASEDIR/bin
mkdir -p "$GOBIN"

go mod download k8s.io/code-generator
CODEGEN_DIR=$(go list -m -f '{{.Dir}}' k8s.io/code-generator)
go install "$CODEGEN_DIR"/cmd/*

TEMPDIR=$BASEDIR/tmp/gen
trap 'rm -rf "$TEMPDIR"' EXIT
mkdir -p "$TEMPDIR"

mkdir -p "$TEMPDIR"/apis/core.cs.sap.com
ln -s "$BASEDIR"/api/v1alpha1 "$TEMPDIR"/apis/core.cs.sap.com/v1alpha1

"$GOBIN"/client-gen \
  --clientset-name versioned \
  --input-base "$TEMPDIR"/apis \
  --input core.cs.sap.com/v1alpha1 \
  --go-header-file "$BASEDIR"/hack/boilerplate.go.txt \
  --output-pkg github.com/sap/project-operator/pkg/client/clientset \
  --output-dir "$TEMPDIR"/pkg/client/clientset \
  --plural-exceptions Project:projects

"$GOBIN"/lister-gen \
  --go-header-file "$BASEDIR"/hack/boilerplate.go.txt \
  --output-pkg github.com/sap/project-operator/pkg/client/listers \
  --output-dir "$TEMPDIR"/pkg/client/listers \
  --plural-exceptions Project:projects \
  github.com/sap/project-operator/tmp/gen/apis/core.cs.sap.com/v1alpha1

"$GOBIN"/informer-gen \
  --versioned-clientset-package github.com/sap/project-operator/pkg/client/clientset/versioned \
  --listers-package github.com/sap/project-operator/pkg/client/listers \
  --go-header-file "$BASEDIR"/hack/boilerplate.go.txt \
  --output-pkg github.com/sap/project-operator/pkg/client/informers \
  --output-dir "$TEMPDIR"/pkg/client/informers \
  --plural-exceptions Project:projects \
  github.com/sap/project-operator/tmp/gen/apis/core.cs.sap.com/v1alpha1

find "$TEMPDIR"/pkg/client -name "*.go" -exec \
  perl -pi -e "s#github\.com/sap/project-operator/tmp/gen/apis/core\.cs\.sap\.com/v1alpha1#github.com/sap/project-operator/api/v1alpha1#g" \
  {} +

rm -rf "$BASEDIR"/pkg/client
mv "$TEMPDIR"/pkg/client "$BASEDIR"/pkg

cd "$BASEDIR"
go mod tidy
go fmt ./pkg/client/...
go vet ./pkg/client/...

