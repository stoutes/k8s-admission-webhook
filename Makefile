IMAGE ?= yourregistry/admission-webhook
TAG   ?= latest

.PHONY: build test docker-build deploy undeploy cert-manager-install verify

build:
	go build ./...

test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

docker-build:
	docker build -t $(IMAGE):$(TAG) .

docker-push:
	docker push $(IMAGE):$(TAG)

## cert-manager-install: installs cert-manager CRDs and controller
cert-manager-install:
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
	kubectl rollout status deployment/cert-manager -n cert-manager --timeout=90s
	kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout=90s
	kubectl rollout status deployment/cert-manager-cainjector -n cert-manager --timeout=90s

## deploy: applies all manifests in dependency order
deploy:
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/rbac/rbac.yaml
	kubectl apply -f deploy/cert-manager/certs.yaml
	kubectl apply -f deploy/webhook/deployment.yaml
	kubectl rollout status deployment/admission-webhook -n webhook-system --timeout=60s
	kubectl apply -f deploy/webhook/webhooks.yaml

## undeploy: removes everything (leaves cert-manager in place)
undeploy:
	kubectl delete -f deploy/webhook/webhooks.yaml --ignore-not-found
	kubectl delete -f deploy/webhook/deployment.yaml --ignore-not-found
	kubectl delete -f deploy/cert-manager/certs.yaml --ignore-not-found
	kubectl delete -f deploy/rbac/rbac.yaml --ignore-not-found
	kubectl delete -f deploy/namespace.yaml --ignore-not-found

## verify: smoke test — label default namespace and create a test pod
verify:
	kubectl label namespace default webhook-injection=enabled --overwrite
	kubectl run webhook-test --image=nginx:alpine --restart=Never --rm -it \
		--overrides='{"spec":{"terminationGracePeriodSeconds":0}}' -- true || true
	@echo "\nContainers in test pod (expect: webhook-test + envoy-sidecar):"
	kubectl get pod webhook-test -o jsonpath='{.spec.containers[*].name}' 2>/dev/null || echo "pod already cleaned up"
	kubectl label namespace default webhook-injection- --overwrite
