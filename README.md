# kubectl-can

> Instantly see what your current Kubernetes context can and cannot do.

**Status:** 🚧 In development

## Overview

krew plugin that shows what the current ServiceAccount/context is allowed to do via SelfSubjectAccessReview.

## Features

- Enumerate common verbs/resources and probe them via SelfSubjectAccessReview
- Group results by resource with allowed/denied markers
- Support namespace scoping and `--as` impersonation
- Filter by verb or resource
- Package as a krew-installable plugin (`kubectl can`)

## Stack

Go 1.22, `client-go`, `cobra`, krew plugin manifest.

## Usage

```bash
kubectl krew install can
kubectl can --namespace prod
```

## License

MIT
