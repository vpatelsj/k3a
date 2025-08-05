go build -o k3a ./cmd/k3a && echo "Build successful"
# Create cluster with private PostgreSQL access (default)
./k3a cluster create --subscription 110efc33-11a4-46b9-9986-60716283fbe7 --region canadacentral --cluster k3s-canadacentral-vapa17 --postgres-sku Standard_D16s_v3
# Or create cluster with public PostgreSQL access (uncomment below and comment above)
# ./k3a cluster create --subscription 110efc33-11a4-46b9-9986-60716283fbe7 --region canadacentral --cluster k3s-canadacentral-vapa17 --postgres-sku Standard_D16s_v3 --postgres-public-access
./k3a pool create --cluster k3s-canadacentral-vapa17 --name k3s-master --instance-count 3 --sku Standard_D16s_v3  --subscription 110efc33-11a4-46b9-9986-60716283fbe7 --role control-plane
./k3a nsg rule create --cluster  k3s-canadacentral-vapa17 --source CorpNetPublic --name AllowCorpNetPublic --priority 150  --subscription 110efc33-11a4-46b9-9986-60716283fbe7
./k3a kubeconfig --cluster  k3s-canadacentral-vapa17

kubectl get nodes -o name | grep "k3s-agent-" | xargs -I {} kubectl label {} node-role.kubernetes.io/worker=worker --overwrite
 ./k3a pool create --cluster k3s-canadacentral-vapa18 --name k3s-agent --instance-count 3 --sku Standard_D16s_v3  --subscription 110efc33-11a4-46b9-9986-60716283fbe7 --role worker