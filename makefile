
.PHONY: prd

dev:
	kubectx dev
	go run cmd/checkingK8sResources/main.go

alp:
	kubectx alp
	go run cmd/checkingK8sResources/main.go

prd:
	kubectx prd
	go run cmd/checkingK8sResources/main.go
