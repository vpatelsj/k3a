package pool

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"golang.org/x/crypto/ssh"
	
	kstrings "github.com/jwilder/k3a/pkg/strings"
)

// KubeadmInstaller handles kubeadm installation and cluster setup
type KubeadmInstaller struct {
	subscriptionID string
	cluster        string
	keyVaultName   string
	sshClient      *ssh.Client
	credential     *azidentity.DefaultAzureCredential
}

// NewKubeadmInstaller creates a new kubeadm installer
func NewKubeadmInstaller(subscriptionID, cluster, keyVaultName string, sshClient *ssh.Client, cred *azidentity.DefaultAzureCredential) *KubeadmInstaller {
	return &KubeadmInstaller{
		subscriptionID: subscriptionID,
		cluster:        cluster,
		keyVaultName:   keyVaultName,
		sshClient:      sshClient,
		credential:     cred,
	}
}

// executeCommand executes a command over SSH and returns the output
func (k *KubeadmInstaller) executeCommand(command string) (string, error) {
	session, err := k.sshClient.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return string(output), fmt.Errorf("command failed: %s, error: %w", string(output), err)
	}
	return string(output), nil
}

// waitForSecretInKeyVault waits for a secret to be available in Key Vault
func (k *KubeadmInstaller) waitForSecretInKeyVault(ctx context.Context, secretName string, maxAttempts int) (string, error) {
	client, err := azsecrets.NewClient(fmt.Sprintf("https://%s.vault.azure.net/", k.keyVaultName), k.credential, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := client.GetSecret(ctx, secretName, "", nil)
		if err == nil && resp.Value != nil {
			fmt.Printf("Secret '%s' found after %d attempts\n", secretName, attempt)
			return *resp.Value, nil
		}

		if attempt < maxAttempts {
			fmt.Printf("Attempt %d/%d: Secret '%s' not found, waiting 30 seconds...\n", attempt, maxAttempts, secretName)
			time.Sleep(30 * time.Second)
		}
	}

	return "", fmt.Errorf("secret '%s' not available after %d attempts", secretName, maxAttempts)
}

// storeSecretInKeyVault stores a secret in Key Vault
func (k *KubeadmInstaller) storeSecretInKeyVault(ctx context.Context, secretName, secretValue string) error {
	client, err := azsecrets.NewClient(fmt.Sprintf("https://%s.vault.azure.net/", k.keyVaultName), k.credential, nil)
	if err != nil {
		return fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	_, err = client.SetSecret(ctx, secretName, azsecrets.SetSecretParameters{Value: &secretValue}, nil)
	if err != nil {
		// Check if it's a soft-delete conflict error
		if strings.Contains(err.Error(), "ObjectIsDeletedButRecoverable") {
			fmt.Printf("Secret '%s' is soft-deleted, attempting to purge and retry...\n", secretName)

			// Try to purge the soft-deleted secret
			if purgeErr := k.purgeDeletedSecret(ctx, secretName); purgeErr != nil {
				fmt.Printf("Warning: failed to purge soft-deleted secret '%s': %v\n", secretName, purgeErr)
			}

			// Wait a moment for the purge to take effect
			time.Sleep(2 * time.Second)

			// Retry storing the secret
			_, err = client.SetSecret(ctx, secretName, azsecrets.SetSecretParameters{Value: &secretValue}, nil)
			if err != nil {
				return fmt.Errorf("failed to store secret '%s' after purge attempt: %w", secretName, err)
			}
		} else {
			return fmt.Errorf("failed to store secret '%s': %w", secretName, err)
		}
	}

	fmt.Printf("Secret '%s' stored in Key Vault\n", secretName)
	return nil
}

// purgeDeletedSecret attempts to purge a soft-deleted secret
func (k *KubeadmInstaller) purgeDeletedSecret(ctx context.Context, secretName string) error {
	// Use the REST API directly for purging since the SDK might not have this operation
	// This requires the Key Vault Contributor role or Key Vault Administrator role
	cmd := fmt.Sprintf("az keyvault secret purge --vault-name %s --name %s", k.keyVaultName, secretName)
	_, err := k.executeCommand(cmd)
	return err
}

// checkAPIServerHealth checks if the API server is reachable
func (k *KubeadmInstaller) checkAPIServerHealth(endpoint string) bool {
	// Extract host and port (default to 6443 if no port specified)
	host := endpoint
	port := "6443"
	if strings.Contains(endpoint, ":") {
		parts := strings.Split(endpoint, ":")
		host = parts[0]
		port = parts[1]
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// validateExistingCluster validates if there's a healthy existing cluster
func (k *KubeadmInstaller) validateExistingCluster(ctx context.Context) bool {
	client, err := azsecrets.NewClient(fmt.Sprintf("https://%s.vault.azure.net/", k.keyVaultName), k.credential, nil)
	if err != nil {
		return false
	}

	// Check if worker join token exists
	workerJoinSecretName := fmt.Sprintf("%s-worker-join", k.cluster)
	_, err = client.GetSecret(ctx, workerJoinSecretName, "", nil)
	if err != nil {
		return false
	}

	// Check if API endpoint exists and is reachable
	apiEndpointSecretName := fmt.Sprintf("%s-api-endpoint", k.cluster)
	resp, err := client.GetSecret(ctx, apiEndpointSecretName, "", nil)
	if err != nil || resp.Value == nil {
		fmt.Println("Warning: Join tokens exist but no API endpoint found")
		return false
	}

	if !k.checkAPIServerHealth(*resp.Value) {
		fmt.Printf("Warning: API server at %s is unreachable\n", *resp.Value)
		return false
	}

	fmt.Printf("Existing cluster validated - API server at %s is reachable\n", *resp.Value)
	return true
}

// cleanupStaleTokens removes stale tokens from Key Vault
func (k *KubeadmInstaller) cleanupStaleTokens(ctx context.Context) error {
	client, err := azsecrets.NewClient(fmt.Sprintf("https://%s.vault.azure.net/", k.keyVaultName), k.credential, nil)
	if err != nil {
		return fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	secrets := []string{
		fmt.Sprintf("%s-worker-join", k.cluster),
		fmt.Sprintf("%s-master-join", k.cluster),
		fmt.Sprintf("%s-api-endpoint", k.cluster),
	}

	fmt.Println("Cleaning up stale kubeadm tokens from Key Vault...")
	for _, secretName := range secrets {
		fmt.Printf("Removing stale secret: %s\n", secretName)

		// Delete secret (if it exists)
		_, _ = client.DeleteSecret(ctx, secretName, nil)

		// Also attempt to purge any soft-deleted version to prevent conflicts
		_ = k.purgeDeletedSecret(ctx, secretName)
	}

	return nil
}

// isNodeBootstrapped checks if the node is already configured with Kubernetes components
func (k *KubeadmInstaller) isNodeBootstrapped() bool {
	// Check if kubeadm is installed and working
	output, err := k.executeCommand("kubeadm version --output=short 2>/dev/null")
	if err != nil {
		// If kubeadm version fails, check if binary exists
		_, err2 := k.executeCommand("which kubeadm")
		if err2 != nil {
			return false
		}
	}

	// Check if kubelet service exists and is installed
	_, err = k.executeCommand("which kubelet")
	if err != nil {
		return false
	}

	// Check if containerd is installed and available
	_, err = k.executeCommand("which containerd")
	if err != nil {
		return false
	}

	fmt.Printf("Node is already bootstrapped with kubeadm %s\n", strings.TrimSpace(output))

	// Check and configure firewall rules if needed
	if err := k.ensureFirewallRules(); err != nil {
		fmt.Printf("Warning: Failed to configure firewall rules: %v\n", err)
	}

	return true
}

// ensureFirewallRules checks if required Kubernetes ports are open and configures them if needed
func (k *KubeadmInstaller) ensureFirewallRules() error {
	// Define required ports for Kubernetes
	requiredPorts := []struct {
		port     string
		protocol string
		desc     string
	}{
		{"6443", "tcp", "API server"},
		{"2379:2380", "tcp", "etcd"},
		{"10250", "tcp", "kubelet"},
		{"10259", "tcp", "kube-scheduler"},
		{"10257", "tcp", "kube-controller-manager"},
		{"10244", "udp", "flannel VXLAN"},
		{"8472", "udp", "flannel VXLAN alt"},
	}

	needsConfiguration := false

	// Check if ports are already allowed
	for _, port := range requiredPorts {
		cmd := fmt.Sprintf("sudo iptables -C INPUT -p %s --dport %s -j ACCEPT 2>/dev/null", port.protocol, port.port)
		_, err := k.executeCommand(cmd)
		if err != nil {
			// Rule doesn't exist, we need to add it
			needsConfiguration = true
			break
		}
	}

	if !needsConfiguration {
		fmt.Println("Firewall rules are already configured")
		return nil
	}

	fmt.Println("Configuring firewall rules for Kubernetes...")

	// Apply firewall rules
	firewallCommands := []string{
		"sudo iptables -I INPUT -p tcp --dport 6443 -j ACCEPT",        // API server
		"sudo iptables -I INPUT -p tcp --dport 2379:2380 -j ACCEPT",   // etcd server client API
		"sudo iptables -I INPUT -p tcp --dport 10250 -j ACCEPT",       // Kubelet API
		"sudo iptables -I INPUT -p tcp --dport 10259 -j ACCEPT",       // kube-scheduler
		"sudo iptables -I INPUT -p tcp --dport 10257 -j ACCEPT",       // kube-controller-manager
		"sudo iptables -I INPUT -p tcp --dport 30000:32767 -j ACCEPT", // NodePort Services
		"sudo iptables -I INPUT -p udp --dport 10244 -j ACCEPT",       // Flannel VXLAN
		"sudo iptables -I INPUT -p udp --dport 8472 -j ACCEPT",        // Flannel VXLAN (alternative)
		"sudo iptables -I INPUT -i flannel.1 -j ACCEPT",               // Allow flannel interface
		"sudo iptables -I INPUT -i cni0 -j ACCEPT",                    // Allow CNI interface
		"sudo mkdir -p /etc/iptables",
		"sudo sh -c 'iptables-save > /etc/iptables/rules.v4'",
	}

	for _, cmd := range firewallCommands {
		_, err := k.executeCommand(cmd)
		if err != nil {
			return fmt.Errorf("failed to execute firewall command '%s': %w", cmd, err)
		}
	}

	fmt.Println("Firewall rules configured successfully")
	return nil
}

// isNodeInCluster checks if the node is already part of a Kubernetes cluster
func (k *KubeadmInstaller) isNodeInCluster() bool {
	// Check if Kubernetes API server port is in use (most reliable indicator)
	_, err := k.executeCommand("ss -tlnp | grep :6443")
	if err == nil {
		fmt.Println("Node is already part of a Kubernetes cluster (API server port 6443 in use)")
		return true
	}

	// Check if kubelet is running and connected to a cluster
	_, err = k.executeCommand("systemctl is-active kubelet 2>/dev/null")
	if err == nil {
		// If kubelet is active, check if it has cluster config
		_, err = k.executeCommand("test -f /etc/kubernetes/kubelet.conf")
		if err == nil {
			fmt.Println("Node is already part of a Kubernetes cluster (kubelet active with config)")
			return true
		}
	}

	// Check if etcd data directory exists and is not empty
	_, err = k.executeCommand("test -d /var/lib/etcd && [ \"$(ls -A /var/lib/etcd)\" ]")
	if err == nil {
		fmt.Println("Node is already part of a Kubernetes cluster (etcd data exists)")
		return true
	}

	// Check if Kubernetes manifests exist
	_, err = k.executeCommand("test -f /etc/kubernetes/manifests/kube-apiserver.yaml")
	if err == nil {
		fmt.Println("Node is already part of a Kubernetes cluster (API server manifest exists)")
		return true
	}

	return false
}

// installKubeadmPrerequisites installs all prerequisites for kubeadm
func (k *KubeadmInstaller) installKubeadmPrerequisites() error {
	fmt.Println("Installing kubeadm prerequisites...")

	commands := []string{
		// Update system and clean package cache
		"sudo tdnf clean all",
		"sudo tdnf update -y",
		"sudo tdnf install -y curl ca-certificates",

		// Install container runtime components separately
		"sudo tdnf install -y moby-runc",
		"sudo tdnf install -y moby-containerd",

		// Start and enable containerd
		"sudo systemctl enable --now containerd",

		// Configure containerd to use systemd cgroup driver
		"sudo mkdir -p /etc/containerd",
		"sudo containerd config default | sudo tee /etc/containerd/config.toml",
		"sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/g' /etc/containerd/config.toml",
		"sudo systemctl restart containerd",

		// Configure kernel modules
		"sudo tee /etc/modules-load.d/k8s.conf <<EOF\noverlay\nbr_netfilter\nEOF",
		"sudo modprobe overlay",
		"sudo modprobe br_netfilter",

		// Configure sysctl parameters
		"sudo tee /etc/sysctl.d/k8s.conf <<EOF\nnet.bridge.bridge-nf-call-iptables  = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\nnet.ipv4.ip_forward                 = 1\nEOF",
		"sudo sysctl --system",

		// Configure iptables to allow Kubernetes ports (CBL-Mariner compatible)
		"sudo iptables -I INPUT -p tcp --dport 6443 -j ACCEPT",        // API server
		"sudo iptables -I INPUT -p tcp --dport 2379:2380 -j ACCEPT",   // etcd server client API
		"sudo iptables -I INPUT -p tcp --dport 10250 -j ACCEPT",       // Kubelet API
		"sudo iptables -I INPUT -p tcp --dport 10259 -j ACCEPT",       // kube-scheduler
		"sudo iptables -I INPUT -p tcp --dport 10257 -j ACCEPT",       // kube-controller-manager
		"sudo iptables -I INPUT -p tcp --dport 30000:32767 -j ACCEPT", // NodePort Services
		"sudo iptables -I INPUT -p udp --dport 10244 -j ACCEPT",       // Flannel VXLAN
		"sudo iptables -I INPUT -p udp --dport 8472 -j ACCEPT",        // Flannel VXLAN (alternative)
		"sudo iptables -I INPUT -p tcp --dport 179 -j ACCEPT",         // BGP (if using Calico)
		"sudo iptables -I INPUT -i flannel.1 -j ACCEPT",               // Allow flannel interface
		"sudo iptables -I INPUT -i cni0 -j ACCEPT",                    // Allow CNI interface
		// Create iptables directory and save rules (CBL-Mariner method)
		"sudo mkdir -p /etc/iptables",
		"sudo sh -c 'iptables-save > /etc/iptables/rules.v4'",

		// Disable swap
		"sudo swapoff -a",
		"sudo sed -i '/ swap / s/^\\(.*\\)$/#\\1/g' /etc/fstab",

		// Add Kubernetes repository
		"sudo tee /etc/yum.repos.d/kubernetes.repo <<EOF\n[kubernetes]\nname=Kubernetes\nbaseurl=https://pkgs.k8s.io/core:/stable:/v1.33/rpm/\nenabled=1\ngpgcheck=1\ngpgkey=https://pkgs.k8s.io/core:/stable:/v1.33/rpm/repodata/repomd.xml.key\nEOF",

		// Install Kubernetes components
		"sudo tdnf install -y kubelet kubeadm kubectl",
		"sudo systemctl enable kubelet",
	}

	for _, command := range commands {
		fmt.Printf("Executing: %s\n", command)
		output, err := k.executeCommand(command)
		if err != nil {
			return fmt.Errorf("failed to execute command '%s': %s, error: %w", command, output, err)
		}
	}

	fmt.Println("Kubeadm prerequisites installed successfully")
	return nil
}

// loginToAzure logs in to Azure using managed identity
func (k *KubeadmInstaller) loginToAzure() error {
	fmt.Println("Logging in to Azure using managed identity...")

	_, err := k.executeCommand("az login --identity")
	if err != nil {
		return fmt.Errorf("failed to login to Azure: %w", err)
	}

	fmt.Println("Successfully logged in to Azure")
	return nil
}

// getSecretFromKeyVault retrieves a secret from Key Vault
func (k *KubeadmInstaller) getSecretFromKeyVault(ctx context.Context, secretName string) (string, error) {
	client, err := azsecrets.NewClient(fmt.Sprintf("https://%s.vault.azure.net/", k.keyVaultName), k.credential, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Key Vault client: %w", err)
	}

	resp, err := client.GetSecret(ctx, secretName, "", nil)
	if err != nil {
		return "", err
	}

	return *resp.Value, nil
}

// ensureJoinTokensExist generates and stores join tokens if they don't already exist
func (k *KubeadmInstaller) ensureJoinTokensExist(ctx context.Context) error {
	// First, ensure firewall rules are configured on the first master
	if err := k.ensureFirewallRules(); err != nil {
		fmt.Printf("Warning: Failed to configure firewall rules: %v\n", err)
	}

	// Check if tokens already exist in Key Vault
	workerJoinSecretName := fmt.Sprintf("%s-worker-join", k.cluster)
	masterJoinSecretName := fmt.Sprintf("%s-master-join", k.cluster)

	// Try to get existing tokens
	_, err := k.getSecretFromKeyVault(ctx, workerJoinSecretName)
	workerTokenExists := (err == nil)

	_, err = k.getSecretFromKeyVault(ctx, masterJoinSecretName)
	masterTokenExists := (err == nil)

	if workerTokenExists && masterTokenExists {
		fmt.Println("Join tokens already exist in Key Vault")
		return nil
	}

	fmt.Println("Generating missing join tokens...")

	// Get internal IP for API endpoint
	internalIPOutput, err := k.executeCommand("ip route get 1.1.1.1 | grep -oP 'src \\K\\S+'")
	if err != nil {
		return fmt.Errorf("failed to get internal IP: %w", err)
	}
	internalIP := strings.TrimSpace(internalIPOutput)
	fmt.Printf("Using internal IP: %s\n", internalIP)

	if !workerTokenExists {
		// Generate worker join command
		workerJoinOutput, err := k.executeCommand("sudo kubeadm token create --print-join-command 2>/dev/null")
		if err != nil {
			return fmt.Errorf("failed to generate worker join token: %w", err)
		}
		workerJoin := strings.TrimSpace(workerJoinOutput)

		if err := k.storeSecretInKeyVault(ctx, workerJoinSecretName, workerJoin); err != nil {
			return err
		}
		fmt.Println("Generated and stored worker join token")
	}

	if !masterTokenExists {
		// Generate certificate key for master join
		certKeyOutput, err := k.executeCommand("sudo kubeadm init phase upload-certs --upload-certs 2>/dev/null | tail -1")
		if err != nil {
			return fmt.Errorf("failed to generate certificate key: %w", err)
		}
		certKey := strings.TrimSpace(certKeyOutput)

		// Get worker join command (either from Key Vault or generate new one)
		var workerJoin string
		if workerTokenExists {
			workerJoin, err = k.getSecretFromKeyVault(ctx, workerJoinSecretName)
			if err != nil {
				return fmt.Errorf("failed to get worker join token: %w", err)
			}
		} else {
			workerJoinOutput, err := k.executeCommand("sudo kubeadm token create --print-join-command 2>/dev/null")
			if err != nil {
				return fmt.Errorf("failed to generate worker join token: %w", err)
			}
			workerJoin = strings.TrimSpace(workerJoinOutput)
		}

		masterJoin := fmt.Sprintf("%s --control-plane --certificate-key %s", workerJoin, certKey)
		if err := k.storeSecretInKeyVault(ctx, masterJoinSecretName, masterJoin); err != nil {
			return err
		}
		fmt.Println("Generated and stored master join token")
	}

	// Store API server endpoint if it doesn't exist
	apiEndpointSecretName := fmt.Sprintf("%s-api-endpoint", k.cluster)
	_, err = k.getSecretFromKeyVault(ctx, apiEndpointSecretName)
	if err != nil {
		apiEndpoint := fmt.Sprintf("%s:6443", internalIP)
		if err := k.storeSecretInKeyVault(ctx, apiEndpointSecretName, apiEndpoint); err != nil {
			return err
		}
		fmt.Println("Stored API server endpoint")
	}

	fmt.Println("Join tokens are now available for other nodes")
	return nil
}

// InstallAsFirstMaster installs kubeadm and bootstraps the first master node
func (k *KubeadmInstaller) InstallAsFirstMaster(ctx context.Context) error {
	fmt.Println("=== BOOTSTRAPPING FIRST MASTER NODE ===")

	// Check if node is already part of a cluster
	if k.isNodeInCluster() {
		fmt.Println("Node is already part of a cluster, skipping initialization")
		// But still need to ensure join tokens exist for other nodes
		if err := k.loginToAzure(); err != nil {
			return err
		}
		return k.ensureJoinTokensExist(ctx)
	}

	// Check if node is already bootstrapped, if not install prerequisites
	if !k.isNodeBootstrapped() {
		if err := k.installKubeadmPrerequisites(); err != nil {
			return err
		}
	} else {
		fmt.Println("Node is already bootstrapped, skipping prerequisite installation")
	}

	// Login to Azure
	if err := k.loginToAzure(); err != nil {
		return err
	}

	// Clean up any existing stale tokens before bootstrapping
	fmt.Println("Ensuring clean state by removing any existing tokens...")
	if err := k.cleanupStaleTokens(ctx); err != nil {
		return err
	}

	// Get internal IP address
	output, err := k.executeCommand("ip route get 8.8.8.8 | awk '{print $7; exit}'")
	if err != nil {
		return fmt.Errorf("failed to get internal IP: %w", err)
	}
	internalIP := strings.TrimSpace(output)
	fmt.Printf("Using internal IP: %s\n", internalIP)

	// Get load balancer public IP for control plane endpoint
	// Create VMSS manager to get load balancer IP
	vmssManager := NewVMSSManager(k.subscriptionID, k.cluster, k.credential)
	clusterHash := kstrings.UniqueString(k.cluster)
	lbName := fmt.Sprintf("k3alb%s", clusterHash)
	
	lbPublicIP, err := vmssManager.GetLoadBalancerPublicIP(context.Background(), lbName)
	if err != nil {
		return fmt.Errorf("failed to get load balancer public IP for control plane endpoint: %w", err)
	}
	controlPlaneEndpoint := fmt.Sprintf("%s:6443", lbPublicIP)
	fmt.Printf("Using control plane endpoint: %s\n", controlPlaneEndpoint)

	// Initialize Kubernetes cluster with stable control plane endpoint
	fmt.Println("Initializing Kubernetes cluster...")
	initCommand := fmt.Sprintf("sudo kubeadm init --pod-network-cidr=10.244.0.0/16 --apiserver-advertise-address=%s --control-plane-endpoint=%s --upload-certs", internalIP, controlPlaneEndpoint)
	_, err = k.executeCommand(initCommand)
	if err != nil {
		return fmt.Errorf("failed to initialize Kubernetes cluster: %w", err)
	}

	// Configure kubectl for azureuser
	fmt.Println("Configuring kubectl for azureuser...")
	kubectlCommands := []string{
		"mkdir -p /home/azureuser/.kube",
		"sudo cp -i /etc/kubernetes/admin.conf /home/azureuser/.kube/config",
		"sudo chown azureuser:azureuser /home/azureuser/.kube/config",
	}

	for _, command := range kubectlCommands {
		if _, err := k.executeCommand(command); err != nil {
			return fmt.Errorf("failed to configure kubectl: %w", err)
		}
	}

	// Install Flannel CNI plugin
	fmt.Println("Installing Flannel CNI plugin...")
	_, err = k.executeCommand("kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml")
	if err != nil {
		return fmt.Errorf("failed to install Flannel CNI: %w", err)
	}

	// Wait for system to stabilize
	fmt.Println("Waiting for cluster to stabilize...")
	time.Sleep(60 * time.Second)

	// Generate and store join tokens in Key Vault
	fmt.Println("Generating and storing join tokens...")

	// Worker join command
	workerJoinOutput, err := k.executeCommand("sudo kubeadm token create --print-join-command 2>/dev/null")
	if err != nil {
		return fmt.Errorf("failed to generate worker join token: %w", err)
	}
	workerJoin := strings.TrimSpace(workerJoinOutput)

	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-worker-join", k.cluster), workerJoin); err != nil {
		return err
	}

	// Master join command (with certificate key)
	certKeyOutput, err := k.executeCommand("sudo kubeadm init phase upload-certs --upload-certs 2>/dev/null | tail -1")
	if err != nil {
		return fmt.Errorf("failed to generate certificate key: %w", err)
	}
	certKey := strings.TrimSpace(certKeyOutput)

	masterJoin := fmt.Sprintf("%s --control-plane --certificate-key %s", workerJoin, certKey)
	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-master-join", k.cluster), masterJoin); err != nil {
		return err
	}

	// Store API server endpoint
	apiEndpoint := fmt.Sprintf("%s:6443", internalIP)
	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-api-endpoint", k.cluster), apiEndpoint); err != nil {
		return err
	}

	fmt.Println("First master node setup completed successfully!")
	return nil
}

// InstallAsAdditionalMaster installs kubeadm and joins as additional master node
func (k *KubeadmInstaller) InstallAsAdditionalMaster(ctx context.Context) error {
	fmt.Println("=== JOINING AS ADDITIONAL MASTER NODE ===")

	// Check if node is already part of a cluster
	if k.isNodeInCluster() {
		fmt.Println("Node is already part of a cluster, skipping join process")
		return nil
	}

	// Check if node is already bootstrapped, if not install prerequisites
	if !k.isNodeBootstrapped() {
		if err := k.installKubeadmPrerequisites(); err != nil {
			return err
		}
	} else {
		fmt.Println("Node is already bootstrapped, skipping prerequisite installation")
	}

	// Login to Azure
	if err := k.loginToAzure(); err != nil {
		return err
	}

	// Wait for master join token to be available
	masterJoinSecretName := fmt.Sprintf("%s-master-join", k.cluster)
	masterJoin, err := k.waitForSecretInKeyVault(ctx, masterJoinSecretName, 60)
	if err != nil {
		return err
	}

	// Join cluster as additional control-plane node
	fmt.Println("Joining cluster as additional control-plane node...")

	// Clean up the join command by removing newlines and extra whitespace
	cleanedMasterJoin := strings.ReplaceAll(masterJoin, "\n", " ")
	cleanedMasterJoin = strings.ReplaceAll(cleanedMasterJoin, "\r", " ")
	// Replace multiple spaces with single space
	cleanedMasterJoin = strings.Join(strings.Fields(cleanedMasterJoin), " ")

	// Properly format the join command by wrapping it in quotes to handle special characters
	joinCommand := fmt.Sprintf("sudo bash -c \"%s\"", strings.ReplaceAll(cleanedMasterJoin, "\"", "\\\""))
	fmt.Printf("Executing join command: %s\n", joinCommand)
	_, err = k.executeCommand(joinCommand)
	if err != nil {
		return fmt.Errorf("failed to join cluster as master: %w", err)
	}

	// Configure kubectl for azureuser
	fmt.Println("Configuring kubectl for azureuser...")
	kubectlCommands := []string{
		"mkdir -p /home/azureuser/.kube",
		"sudo cp -i /etc/kubernetes/admin.conf /home/azureuser/.kube/config",
		"sudo chown azureuser:azureuser /home/azureuser/.kube/config",
	}

	for _, command := range kubectlCommands {
		if _, err := k.executeCommand(command); err != nil {
			return fmt.Errorf("failed to configure kubectl: %w", err)
		}
	}

	fmt.Println("Additional master node joined successfully!")
	return nil
}

// InstallAsWorker installs kubeadm and joins as worker node
func (k *KubeadmInstaller) InstallAsWorker(ctx context.Context) error {
	fmt.Println("=== JOINING AS WORKER NODE ===")

	// Check if node is already part of a cluster
	if k.isNodeInCluster() {
		fmt.Println("Node is already part of a cluster, skipping join process")
		return nil
	}

	// Check if node is already bootstrapped, if not install prerequisites
	if !k.isNodeBootstrapped() {
		if err := k.installKubeadmPrerequisites(); err != nil {
			return err
		}
	} else {
		fmt.Println("Node is already bootstrapped, skipping prerequisite installation")
	}

	// Login to Azure
	if err := k.loginToAzure(); err != nil {
		return err
	}

	// Wait for worker join token to be available
	workerJoinSecretName := fmt.Sprintf("%s-worker-join", k.cluster)
	workerJoin, err := k.waitForSecretInKeyVault(ctx, workerJoinSecretName, 60)
	if err != nil {
		return err
	}

	// Join cluster as worker node
	fmt.Println("Joining cluster as worker node...")

	// Clean up the join command by removing newlines and extra whitespace
	cleanedWorkerJoin := strings.ReplaceAll(workerJoin, "\n", " ")
	cleanedWorkerJoin = strings.ReplaceAll(cleanedWorkerJoin, "\r", " ")
	// Replace multiple spaces with single space
	cleanedWorkerJoin = strings.Join(strings.Fields(cleanedWorkerJoin), " ")

	joinCommand := fmt.Sprintf("sudo %s", cleanedWorkerJoin)
	_, err = k.executeCommand(joinCommand)
	if err != nil {
		return fmt.Errorf("failed to join cluster as worker: %w", err)
	}

	fmt.Println("Worker node joined successfully!")
	return nil
}

// CreateSSHClient creates an SSH client connection to the target VM via load balancer NAT
func CreateSSHClientViaNAT(lbPublicIP string, natPort int, username, privateKeyPath string) (*ssh.Client, error) {
	// Read private key
	if privateKeyPath == "" {
		privateKeyPath = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create SSH config
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, use proper host key verification
		Timeout:         30 * time.Second,
	}

	// Connect to SSH server via load balancer NAT
	address := fmt.Sprintf("%s:%d", lbPublicIP, natPort)
	fmt.Printf("Connecting to SSH via NAT: %s\n", address)

	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server at %s: %w", address, err)
	}

	return client, nil
}

// CreateSSHClient creates an SSH client connection to the target VM (deprecated - use CreateSSHClientViaNAT for VMSS)
func CreateSSHClient(host, username, privateKeyPath string) (*ssh.Client, error) {
	// Read private key
	if privateKeyPath == "" {
		privateKeyPath = filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa")
	}

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %w", err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create SSH config
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, use proper host key verification
		Timeout:         30 * time.Second,
	}

	// Connect to SSH server
	client, err := ssh.Dial("tcp", net.JoinHostPort(host, "22"), config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server: %w", err)
	}

	return client, nil
}
