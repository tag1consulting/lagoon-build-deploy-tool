# Tag1 Fork

A modified version of build-deploy-tool with some added functionality allowing lagoon docker-compose service labels to
specify resource requirements of service.

Branch `upstream` is to be kept up with upstream main. `tag1-main` is where modifications live.

Image may be built and push to ghcr.io by pushing a tag. We should version our tags off of the current upstream version changes are based on.
Currently this is `core-v2.21.0` upstream, so should do something like: `v2.21.0-tag1-0.1` (keep our versioning after `v2.21.0-tag1-` prefix).

## Usage

### Setting resource requirements

To override lagoon's default resource requirements for a service type, label can be specified in docker-compose for respective service.
It may also be overriden to a specific branch environment (if want to raise for prod/stage, for example).

```yaml
php:
  labels:
    lagoon.type: nginx-php-persistent
    lagoon.name: nginx-php

    # Set default resources for php service
    lagoon.resources.requests.cpu: 20m
    lagoon.resources.limits.cpu: 200m
    lagoon.resources.requests.memory: 200Mi
    lagoon.resources.limits.memory: 1Gi

    # Override on main branch
    lagoon.resources.override-branch.main.requests.cpu: 400m
    lagoon.resources.override-branch.main.limits.cpu: 400m
```

### Deploying

To deploy a custom version of this image, in the `lagoon-build-deploy` chart, the `overrideBuildDeployImage` value can be set. ([Context](https://github.com/uselagoon/lagoon-charts/blob/42cf5a20d442036faa6aca2081e74f3fcffcb65c/charts/lagoon-build-deploy/values.yaml#L162C1-L162C25))
`lagoon-build-deploy` chart is a dependency of `lagoon-remote` chart, the value should be specified there.

Alternatively, the deploy target (`lagoon list deploytargets`) has a buildimage override field that may be used.
```bash
lagoon update deploytargets --id <id> --build-image 'ghcr.io/tag1consulting/build-deploy-image:v2.21.0-tag1-0.1'
```
lagoon list projects can be used to verify project is associate with a given deploytarget.

# Build and Deploy Resource Generator

This is a tool used to help with creating resources for Lagoon builds
