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
	// Define required ports for Kubernetes (etcd removed since using external etcd)
	requiredPorts := []struct {
		port     string
		protocol string
		desc     string
	}{
		{"6443", "tcp", "API server"},
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

	// Check if Kubernetes manifests exist
	_, err = k.executeCommand("test -f /etc/kubernetes/manifests/kube-apiserver.yaml")
	if err == nil {
		fmt.Println("Node is already part of a Kubernetes cluster (API server manifest exists)")
		return true
	}

	return false
}

// installKubeadmPrerequisites ensures cloud-init completed and configures dynamic firewall rules
func (k *KubeadmInstaller) installKubeadmPrerequisites() error {
	fmt.Println("Verifying cloud-init completion and configuring firewall...")

	// Wait for cloud-init to complete (check for marker file)
	checkCommand := "test -f /var/lib/cloud/k3a-ready"
	for i := 0; i < 30; i++ { // Wait up to 5 minutes
		_, err := k.executeCommand(checkCommand)
		if err == nil {
			fmt.Println("Cloud-init setup verified - all prerequisites installed")
			break
		}
		if i == 29 {
			return fmt.Errorf("cloud-init did not complete within timeout")
		}
		fmt.Printf("Waiting for cloud-init to complete... (%d/30)\n", i+1)
		time.Sleep(10 * time.Second)
	}

	// Configure dynamic iptables rules (these need to be applied each time)
	firewallCommands := []string{
		// Configure iptables to allow Kubernetes ports (CBL-Mariner compatible)
		// etcd ports removed since using external etcd
		"sudo iptables -I INPUT -p tcp --dport 6443 -j ACCEPT",        // API server
		"sudo iptables -I INPUT -p tcp --dport 10250 -j ACCEPT",       // Kubelet API
		"sudo iptables -I INPUT -p tcp --dport 10259 -j ACCEPT",       // kube-scheduler
		"sudo iptables -I INPUT -p tcp --dport 10257 -j ACCEPT",       // kube-controller-manager
		"sudo iptables -I INPUT -p tcp --dport 30000:32767 -j ACCEPT", // NodePort Services
		"sudo iptables -I INPUT -p udp --dport 10244 -j ACCEPT",       // Flannel VXLAN
		"sudo iptables -I INPUT -p udp --dport 8472 -j ACCEPT",        // Flannel VXLAN (alternative)
		"sudo iptables -I INPUT -p tcp --dport 179 -j ACCEPT",         // BGP (if using Calico)
		"sudo iptables -I INPUT -i flannel.1 -j ACCEPT",               // Allow flannel interface
		"sudo iptables -I INPUT -i cni0 -j ACCEPT",                    // Allow CNI interface
		// Save iptables rules
		"sudo mkdir -p /etc/iptables",
		"sudo sh -c 'iptables-save > /etc/iptables/rules.v4'",
	}

	for _, command := range firewallCommands {
		fmt.Printf("Executing: %s\n", command)
		output, err := k.executeCommand(command)
		if err != nil {
			return fmt.Errorf("failed to execute command '%s': %s, error: %w", command, output, err)
		}
	}

	fmt.Println("Cloud-init verification and firewall configuration completed successfully")
	return nil
}

// waitForAzureCLI waits for Azure CLI to become available
func (k *KubeadmInstaller) waitForAzureCLI() error {
	fmt.Println("Waiting for Azure CLI to become available...")

	for i := 0; i < 60; i++ { // Wait up to 5 minutes
		_, err := k.executeCommand("which az")
		if err == nil {
			fmt.Println("Azure CLI is now available")
			return nil
		}

		// Also try the full path
		_, err = k.executeCommand("test -x /usr/bin/az")
		if err == nil {
			fmt.Println("Azure CLI found at /usr/bin/az")
			return nil
		}

		if i < 59 {
			fmt.Printf("Azure CLI not yet available, waiting... (%d/60)\n", i+1)
			time.Sleep(5 * time.Second)
		}
	}

	return fmt.Errorf("azure CLI did not become available within timeout")
}

// waitForKubeadm waits for kubeadm to become available
func (k *KubeadmInstaller) waitForKubeadm() error {
	fmt.Println("Waiting for kubeadm to become available...")

	for i := 0; i < 60; i++ { // Wait up to 5 minutes
		_, err := k.executeCommand("which kubeadm")
		if err == nil {
			fmt.Println("kubeadm is now available")
			return nil
		}

		// Also try the full path
		_, err = k.executeCommand("test -x /usr/bin/kubeadm")
		if err == nil {
			fmt.Println("kubeadm found at /usr/bin/kubeadm")
			return nil
		}

		if i < 59 {
			fmt.Printf("kubeadm not yet available, waiting... (%d/60)\n", i+1)
			time.Sleep(5 * time.Second)
		}
	}

	return fmt.Errorf("kubeadm did not become available within timeout")
}

// setupDNSResolution sets up local DNS resolution for the cluster endpoint
// This is needed because joining nodes need to resolve the DNS name from kubeadm-config
// but DNS propagation may not be complete yet
func (k *KubeadmInstaller) setupDNSResolution() error {
	fmt.Println("Setting up local DNS resolution for cluster endpoint...")

	// Get the first master's internal IP from Key Vault
	ctx := context.Background()
	apiEndpoint, err := k.getSecretFromKeyVault(ctx, fmt.Sprintf("%s-api-endpoint", k.cluster))
	if err != nil {
		return fmt.Errorf("failed to get API endpoint from Key Vault: %w", err)
	}

	// Extract the DNS name from the endpoint (format: hostname:6443)
	dnsName := strings.Split(apiEndpoint, ":")[0]

	// We need to resolve this DNS name to the first master's internal IP
	// For now, we'll derive the first master IP by getting the master join token and parsing it
	masterJoinSecretName := fmt.Sprintf("%s-master-join", k.cluster)
	masterJoin, err := k.getSecretFromKeyVault(ctx, masterJoinSecretName)
	if err != nil {
		return fmt.Errorf("failed to get master join token: %w", err)
	}

	// Extract the IP from the join command (format: "kubeadm join 10.1.0.5:6443 ...")
	parts := strings.Fields(masterJoin)
	if len(parts) < 3 {
		return fmt.Errorf("invalid master join command format")
	}

	// Get the IP:port from the join command
	joinEndpoint := parts[2]                             // Should be "10.1.0.5:6443"
	firstMasterIP := strings.Split(joinEndpoint, ":")[0] // Extract "10.1.0.5"

	// Add DNS resolution to /etc/hosts
	hostsEntry := fmt.Sprintf("%s %s", firstMasterIP, dnsName)
	addHostsCmd := fmt.Sprintf("echo '%s' | sudo tee -a /etc/hosts", hostsEntry)

	// Check if entry already exists first
	checkCmd := fmt.Sprintf("grep -q '%s' /etc/hosts", dnsName)
	_, err = k.executeCommand(checkCmd)
	if err != nil {
		// Entry doesn't exist, add it
		_, err = k.executeCommand(addHostsCmd)
		if err != nil {
			return fmt.Errorf("failed to add DNS entry to /etc/hosts: %w", err)
		}
		fmt.Printf("Added DNS resolution: %s -> %s\n", dnsName, firstMasterIP)
	} else {
		fmt.Printf("DNS resolution already configured for %s\n", dnsName)
	}

	return nil
}

// loginToAzure logs in to Azure using managed identity
func (k *KubeadmInstaller) loginToAzure() error {
	fmt.Println("Logging in to Azure using managed identity...")

	// Wait for Azure CLI to be available first
	if err := k.waitForAzureCLI(); err != nil {
		return fmt.Errorf("azure CLI not available: %w", err)
	}

	// Use full path to az command and set PATH to ensure it's found
	_, err := k.executeCommand("export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin && /usr/bin/az login --identity")
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

// InstallAsFirstMaster installs kubeadm and bootstraps the first master node
func (k *KubeadmInstaller) InstallAsFirstMaster(ctx context.Context) error {
	fmt.Println("=== BOOTSTRAPPING FIRST MASTER NODE ===")

	// Check if node is already part of a cluster
	if k.isNodeInCluster() {
		fmt.Println("Node is already part of a cluster")
		fmt.Println("Resetting existing cluster to reconfigure for external etcd...")

		// Reset the existing cluster
		resetCmd := "sudo kubeadm reset --force"
		_, err := k.executeCommand(resetCmd)
		if err != nil {
			fmt.Printf("Warning: Failed to reset cluster: %v, proceeding anyway\n", err)
		}

		// Clean up any remaining files and network state
		cleanupCommands := []string{
			"sudo rm -rf /etc/kubernetes/manifests/*",
			"sudo rm -rf /etc/kubernetes/pki/*",
			"sudo rm -rf /var/lib/kubelet/*",
			"sudo rm -rf /var/lib/etcd/*", // Clean up embedded etcd data
			"sudo systemctl stop kubelet",
			"sudo systemctl stop containerd",
			"sudo systemctl start containerd", // Restart containerd to clean containers
		}

		for _, cmd := range cleanupCommands {
			k.executeCommand(cmd) // Ignore errors
		}

		fmt.Println("Cluster reset completed, proceeding with external etcd initialization...")
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

	// Construct the DNS name with correct Azure format
	// Extract region from cluster name (format: k3s-{region}-{suffix})
	clusterParts := strings.Split(k.cluster, "-")
	var region string
	if len(clusterParts) >= 3 && strings.HasPrefix(k.cluster, "k3s-") {
		region = clusterParts[1] // Extract region from k3s-{region}-{suffix}
	} else {
		region = "canadacentral" // fallback
	}
	dnsName := fmt.Sprintf("%s.%s.cloudapp.azure.com", k.cluster, region)
	// Use internal IP for control plane endpoint to avoid external load balancer dependency
	controlPlaneEndpoint := fmt.Sprintf("%s:6443", internalIP)
	fmt.Printf("Using internal IP control plane endpoint: %s\n", controlPlaneEndpoint)
	fmt.Printf("External DNS name for certificates: %s\n", dnsName)

	fmt.Println("Initializing Kubernetes cluster...")

	// Wait for kubeadm to be available first
	if err := k.waitForKubeadm(); err != nil {
		return fmt.Errorf("kubeadm not available: %w", err)
	}

	// Create kubeadm configuration file with external etcd
	fmt.Println("Creating kubeadm configuration file...")

	kubeadmConfig := fmt.Sprintf(`apiVersion: kubeadm.k8s.io/v1beta4
kind: ClusterConfiguration
kubernetesVersion: v1.33.1
controlPlaneEndpoint: "%s"
networking:
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "172.20.0.0/16"
apiServer:
  certSANs:
  - "%s"
  - "%s"
  extraArgs:
  - name: etcd-compaction-interval
    value: "0"
controllerManager:
  extraArgs:
  - name: service-cluster-ip-range
    value: "172.20.0.0/16"
etcd:
  external:
    endpoints:
    - "http://4.206.93.140:2379"
---
apiVersion: kubeadm.k8s.io/v1beta4
kind: InitConfiguration
localAPIEndpoint:
  advertiseAddress: "%s"
  bindPort: 6443
---
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
maxPods: 300
`, controlPlaneEndpoint, internalIP, dnsName, internalIP)

	// Write kubeadm config to temporary file
	configCmd := fmt.Sprintf("cat > /tmp/kubeadm-config.yaml << 'EOF'\n%s\nEOF", kubeadmConfig)
	_, err = k.executeCommand(configCmd)
	if err != nil {
		return fmt.Errorf("failed to create kubeadm config file: %w", err)
	}

	// Initialize Kubernetes cluster using config file
	// Pre-pull recommended pause image version to avoid sandbox mismatch warnings
	_, _ = k.executeCommand("sudo crictl pull registry.k8s.io/pause:3.10 || sudo ctr -n k8s.io images pull registry.k8s.io/pause:3.10 || true")
	initCommand := "sudo kubeadm init --config=/tmp/kubeadm-config.yaml --upload-certs --ignore-preflight-errors=all"
	_, err = k.executeCommand(initCommand)
	if err != nil {
		return fmt.Errorf("failed to initialize Kubernetes cluster: %w", err)
	}

	// Clean up config file
	k.executeCommand("rm -f /tmp/kubeadm-config.yaml")

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

	// Store kubeconfig in Key Vault with load balancer endpoint
	fmt.Println("Storing kubeconfig in Key Vault...")
	kubeconfigOutput, err := k.executeCommand("sudo cat /etc/kubernetes/admin.conf")
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	// Replace the internal IP with load balancer public IP in kubeconfig
	externalEndpoint := fmt.Sprintf("%s:6443", dnsName)
	modifiedKubeconfig := strings.ReplaceAll(kubeconfigOutput, fmt.Sprintf("https://%s:6443", internalIP), fmt.Sprintf("https://%s", externalEndpoint))
	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-kubeconfig", k.cluster), modifiedKubeconfig); err != nil {
		return err
	}
	fmt.Println("Kubeconfig stored in Key Vault with load balancer endpoint")

	// Install Flannel CNI plugin
	fmt.Println("Installing Flannel CNI plugin...")
	_, err = k.executeCommand("kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml")
	if err != nil {
		return fmt.Errorf("failed to install Flannel CNI: %w", err)
	}

	// Wait for system to stabilize
	fmt.Println("Waiting for cluster to stabilize...")
	time.Sleep(60 * time.Second)

	// Patch kubeadm-config ConfigMap to add controlPlaneEndpoint for multi-master support
	fmt.Println("Updating kubeadm configuration for multi-master support...")
	if err := k.patchKubeadmConfigForMultiMaster(controlPlaneEndpoint); err != nil {
		return fmt.Errorf("failed to update kubeadm config for multi-master: %w", err)
	}

	// Generate and store join tokens in Key Vault
	fmt.Println("Generating and storing join tokens...")

	// Worker join command
	workerJoinOutput, err := k.executeCommand("sudo kubeadm token create --print-join-command 2>/dev/null")
	if err != nil {
		return fmt.Errorf("failed to generate worker join token: %w", err)
	}
	workerJoin := strings.TrimSpace(workerJoinOutput)

	// Replace DNS endpoint with internal IP for cluster joining (DNS propagation issue)
	// The join command should use the internal IP of the master, not the load balancer DNS
	dnsEndpoint := fmt.Sprintf("%s:6443", dnsName)
	internalEndpoint := fmt.Sprintf("%s:6443", internalIP)
	workerJoinForCluster := strings.ReplaceAll(workerJoin, dnsEndpoint, internalEndpoint)

	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-worker-join", k.cluster), workerJoinForCluster); err != nil {
		return err
	}

	// Master join command will include certificate key after upload-certs
	certKeyOutput, err := k.executeCommand("sudo kubeadm init phase upload-certs --upload-certs 2>/dev/null | tail -1")
	if err != nil {
		return fmt.Errorf("failed to generate certificate key: %w", err)
	}
	certKey := strings.TrimSpace(certKeyOutput)
	masterJoin := fmt.Sprintf("%s --control-plane --certificate-key %s", workerJoinForCluster, certKey)
	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-master-join", k.cluster), masterJoin); err != nil {
		return err
	}

	// Store API server endpoint (use load balancer public IP for external access)
	apiEndpoint := externalEndpoint // Use external DNS name for client access
	if err := k.storeSecretInKeyVault(ctx, fmt.Sprintf("%s-api-endpoint", k.cluster), apiEndpoint); err != nil {
		return err
	}

	// (legacy block removed – master join secret already written with certificate key)

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

	// Setup DNS resolution for cluster endpoint (needed for kubeadm-config ConfigMap access)
	if err := k.setupDNSResolution(); err != nil {
		return fmt.Errorf("failed to setup DNS resolution: %w", err)
	}

	// Wait for master join token to be available
	masterJoinSecretName := fmt.Sprintf("%s-master-join", k.cluster)
	masterJoin, err := k.waitForSecretInKeyVault(ctx, masterJoinSecretName, 60)
	if err != nil {
		return err
	}

	// Join cluster as additional control-plane node
	fmt.Println("Joining cluster as additional control-plane node...")
	// Pre-pull recommended pause image to align with kubeadm expectations
	_, _ = k.executeCommand("sudo crictl pull registry.k8s.io/pause:3.10 || sudo ctr -n k8s.io images pull registry.k8s.io/pause:3.10 || true")

	// Clean up the join command by removing newlines and extra whitespace
	cleanedMasterJoin := strings.ReplaceAll(masterJoin, "\n", " ")
	cleanedMasterJoin = strings.ReplaceAll(cleanedMasterJoin, "\r", " ")
	// Replace multiple spaces with single space
	cleanedMasterJoin = strings.Join(strings.Fields(cleanedMasterJoin), " ")

	// Standard path: include ignore-preflight to relax system checks
	cleanedMasterJoin = fmt.Sprintf("%s --ignore-preflight-errors=all", cleanedMasterJoin)

	// Execute join (kubeadm will perform download-certs if certificate-key present)
	joinCommand := fmt.Sprintf("sudo bash -c \"%s\"", strings.ReplaceAll(cleanedMasterJoin, "\"", "\\\""))
	fmt.Printf("Executing join command: %s\n", joinCommand)
	output, err2 := k.executeCommand(joinCommand)
	if err2 != nil {
		// Detect cert decryption failure and provide remediation hints
		if strings.Contains(output, "download-certs") || strings.Contains(output, "error decoding secret data") || strings.Contains(output, "message authentication failed") {
			return fmt.Errorf("failed to join cluster as master: unexpected attempt to download certs (legacy path). Ensure PKI bundle secret exists and join command omits --certificate-key. Raw error: %w", err2)
		}
		return fmt.Errorf("failed to join cluster as master: %w", err2)
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

// patchKubeadmConfigForMultiMaster patches the kubeadm-config ConfigMap to add controlPlaneEndpoint
func (k *KubeadmInstaller) patchKubeadmConfigForMultiMaster(controlPlaneEndpoint string) error {
	// Get current kubeadm-config ConfigMap
	getConfigCmd := "kubectl get configmap kubeadm-config -n kube-system -o yaml"
	currentConfig, err := k.executeCommand(getConfigCmd)
	if err != nil {
		return fmt.Errorf("failed to get kubeadm-config ConfigMap: %w", err)
	}

	// Check if controlPlaneEndpoint and external etcd are already set
	if strings.Contains(currentConfig, "controlPlaneEndpoint") && strings.Contains(currentConfig, "external:") {
		fmt.Println("controlPlaneEndpoint and external etcd already configured in kubeadm-config")
		return nil
	}

	fmt.Printf("Adding controlPlaneEndpoint %s and external etcd configuration to kubeadm-config ConfigMap...\n", controlPlaneEndpoint)

	// Get the current ClusterConfiguration data
	getClusterConfigCmd := "kubectl get configmap kubeadm-config -n kube-system -o jsonpath='{.data.ClusterConfiguration}'"
	clusterConfig, err := k.executeCommand(getClusterConfigCmd)
	if err != nil {
		return fmt.Errorf("failed to get ClusterConfiguration: %w", err)
	}

	// Add controlPlaneEndpoint and etcd configuration to the configuration
	// We'll add them right after the apiVersion line
	lines := strings.Split(clusterConfig, "\n")
	var newLines []string
	added := false

	for _, line := range lines {
		newLines = append(newLines, line)
		// Add controlPlaneEndpoint and etcd config after apiVersion line
		if strings.HasPrefix(line, "apiVersion:") && !added {
			newLines = append(newLines, fmt.Sprintf("controlPlaneEndpoint: %s", controlPlaneEndpoint))
			newLines = append(newLines, "etcd:")
			newLines = append(newLines, "  external:")
			newLines = append(newLines, "    endpoints:")
			newLines = append(newLines, "    - http://4.206.93.140:2379")
			added = true
		}
	}

	if !added {
		// If apiVersion wasn't found, add it at the beginning after any initial lines
		newLines = []string{
			fmt.Sprintf("controlPlaneEndpoint: %s", controlPlaneEndpoint),
			"etcd:",
			"  external:",
			"    endpoints:",
			"    - http://4.206.93.140:2379",
		}
		newLines = append(newLines, lines...)
	}

	newConfig := strings.Join(newLines, "\n")

	// Create a temporary file with the new configuration
	tempConfigCmd := fmt.Sprintf("cat > /tmp/cluster-config.yaml << 'EOF'\n%s\nEOF", newConfig)
	_, err = k.executeCommand(tempConfigCmd)
	if err != nil {
		return fmt.Errorf("failed to create temporary config file: %w", err)
	}

	// Update the ConfigMap with the new configuration
	patchCmd := "kubectl create configmap kubeadm-config --from-file=ClusterConfiguration=/tmp/cluster-config.yaml -n kube-system --dry-run=client -o yaml | kubectl apply -f -"
	_, err = k.executeCommand(patchCmd)
	if err != nil {
		return fmt.Errorf("failed to update kubeadm-config ConfigMap: %w", err)
	}

	// Clean up temporary file
	k.executeCommand("rm -f /tmp/cluster-config.yaml")

	fmt.Printf("Successfully updated kubeadm-config with controlPlaneEndpoint: %s and external etcd configuration\n", controlPlaneEndpoint)
	return nil
}
