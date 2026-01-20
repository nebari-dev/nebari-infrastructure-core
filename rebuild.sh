kind delete cluster -n nebari-local
docker network rm kind 2>/dev/null || true
docker network create --subnet=192.168.1.0/24 --gateway=192.168.1.1 kind
kind create cluster --name nebari-local

time go build -o nic ./cmd/nic
./nic deploy -f ./examples/local-config.yaml