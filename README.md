# k3a ![MIT License](https://img.shields.io/badge/license-MIT-green.svg) ![Go Version](https://img.shields.io/badge/Go-1.24%2B-blue.svg) ![Azure](https://img.shields.io/badge/Azure-Ready-brightgreen.svg)

k3a is a command-line tool for deploying and managing lightweight Kubernetes clusters on Azure using [k3s](https://k3s.io/). It aims to reduce operational overhead and complexity, providing a minimal and opinionated approach to cluster management.

---

## Why k3a?

Managing Kubernetes clusters and networking in Azure can be complex. k3a addresses this by being:

- **Simple:** Minimal commands, clear defaults, and no unnecessary options.
- **Minimalistic:** Lightweight, fast, and easy to use—no unnecessary features.
- **Opinionated:** Secure-by-default configurations and best practices are built in.
- **Secure:** Automated NSG rules and sensible defaults to help protect workloads.
- **Microservice-Ready:** Suitable for deploying microservices on Kubernetes, from single-node clusters to larger deployments.
- **Batteries Included & Upgradeable:** Provides a production-ready baseline, but you can add features such as HPA (Horizontal Pod Autoscaler), MSI-based ACR pull, and others as your requirements evolve.

**Intended Users:**
- Developers who need to deploy secure, production-ready clusters quickly
- DevOps engineers who value automation, minimalism, and security
- Teams deploying microservices on Kubernetes
- Anyone seeking a streamlined, opinionated cloud experience

---

## Getting Started

### Prerequisites
- Go 1.24 or later
- Azure CLI installed and authenticated (`az login`)
- Sufficient Azure permissions

### Installation

Clone the repository and build the CLI:

```sh
git clone https://github.com/your-org/k3a.git
cd k3a
go build -o k3a ./cmd
```

Or install directly with Go:

```sh
go install github.com/your-org/k3a/cmd@latest
```

### Configuration

Set the default cluster (resource group) via environment variable:

```sh
export K3A_CLUSTER=my-cluster-name
```

Set the default Azure subscription (optional, if you work with multiple subscriptions):

```sh
export K3A_SUBSCRIPTION=your-azure-subscription-id
```

Or specify the cluster and subscription with the `--cluster` and `--subscription` flags in commands.

## Usage Examples

### Create a New Cluster

```sh
k3a cluster create --name my-cluster --location eastus
```

### List Clusters

```sh
k3a cluster list
```

### Add an NSG Rule

```sh
k3a nsg rule add --nsg-name my-nsg --name allow-ssh --priority 100 --direction Inbound --access Allow --protocol Tcp --source * --source-port * --dest * --dest-port 22
```

### List NSG Rules

```sh
k3a nsg rule list --nsg-name my-nsg
```

### Delete an NSG Rule

```sh
k3a nsg rule delete --nsg-name my-nsg --name allow-ssh
```

### Scale a Node Pool

```sh
k3a pool scale --name my-pool --count 5
```

## Contributing

Contributions are welcome! Please open issues or pull requests for bug fixes, features, or documentation improvements.

## License

This project is licensed under the MIT License.
