kind delete cluster -n nebari-local
kind create cluster --name nebari-local --config - <<EOF                    
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF

time go build -o nic ./cmd/nic
./nic deploy -f ./examples/local-config.yaml