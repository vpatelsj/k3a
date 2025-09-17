package pool

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"os"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/jwilder/k3a/loadbalancer/rule"
	kstrings "github.com/jwilder/k3a/pkg/strings"
)

type CreatePoolArgs struct {
	SubscriptionID string
	Cluster        string
	Location       string
	Role           string
	Name           string
	SSHKeyPath     string
	InstanceCount  int
	K8sVersion     string   // New field for Kubernetes version
	SKU            string   // VM SKU type
	OSDiskSizeGB   int      // OS disk size in GB
	MSIIDs         []string // Additional user-assigned MSI resource IDs
}

//go:embed cloud-init.yaml
var cloudInitFS embed.FS

// getCloudInitData renders the cloud-init template and returns base64-encoded data
func getCloudInitData(tmplData map[string]string) (string, error) {
	cloudInitBytes, err := cloudInitFS.ReadFile("cloud-init.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to read embedded cloud-init.yaml: %w", err)
	}
	tmpl, err := template.New("cloud-init").Parse(string(cloudInitBytes))
	if err != nil {
		return "", fmt.Errorf("failed to parse cloud-init template: %w", err)
	}
	var renderedCloudInit bytes.Buffer
	if err := tmpl.Execute(&renderedCloudInit, tmplData); err != nil {
		return "", fmt.Errorf("failed to render cloud-init template: %w", err)
	}
	return base64.StdEncoding.EncodeToString(renderedCloudInit.Bytes()), nil
}

// getManagedIdentity fetches the managed identity resource
func getManagedIdentity(ctx context.Context, subscriptionID, cluster string, cred *azidentity.DefaultAzureCredential) (*armmsi.Identity, error) {
	msiClient, err := armmsi.NewUserAssignedIdentitiesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create managed identity client: %w", err)
	}
	msiName := "k3a-msi"
	msi, err := msiClient.Get(ctx, cluster, msiName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get managed identity: %w", err)
	}
	return &msi.Identity, nil
}

// getSubnet fetches the subnet resource
func getSubnet(ctx context.Context, subscriptionID, cluster, vnetName string, cred *azidentity.DefaultAzureCredential) (*armnetwork.Subnet, error) {
	subnetClient, err := armnetwork.NewSubnetsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet client: %w", err)
	}
	subnet, err := subnetClient.Get(ctx, cluster, vnetName, "default", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get subnet: %w", err)
	}
	return &subnet.Subnet, nil
}

// getLoadBalancerPools fetches backend and inbound NAT pools for control plane
func getLoadBalancerPools(ctx context.Context, subscriptionID, cluster, lbName, poolName string, cred *azidentity.DefaultAzureCredential) ([]*armcompute.SubResource, []*armcompute.SubResource, error) {
	lbClient, err := armnetwork.NewLoadBalancersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create load balancer client: %w", err)
	}
	lb, err := lbClient.Get(ctx, cluster, lbName, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get load balancer: %w", err)
	}

	// Use poolName in the backend pool name
	backendPoolName := "outbound-pool"
	// Always add the VMSS to the existing backend pool (if found)
	var existingBackendPoolID *string
	if lb.Properties != nil && lb.Properties.BackendAddressPools != nil {
		for _, bp := range lb.Properties.BackendAddressPools {
			if bp.Name != nil && *bp.Name == backendPoolName {
				existingBackendPoolID = bp.ID
				break
			}
		}
	}

	// Create a new backend pool for this VMSS
	newBackendPoolName := fmt.Sprintf("%s-backend-pool", poolName)
	var newBackendPoolID *string
	if lb.Properties != nil && lb.Properties.BackendAddressPools != nil {
		for _, bp := range lb.Properties.BackendAddressPools {
			if bp.Name != nil && *bp.Name == newBackendPoolName {
				newBackendPoolID = bp.ID
				break
			}
		}
	}
	if newBackendPoolID == nil {
		backendPoolsClient, err := armnetwork.NewLoadBalancerBackendAddressPoolsClient(subscriptionID, cred, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get load balancer: %w", err)
		}
		backendPoolParams := armnetwork.BackendAddressPool{
			Name: to.Ptr(newBackendPoolName),
		}
		backendPoolPoller, err := backendPoolsClient.BeginCreateOrUpdate(ctx, cluster, lbName, newBackendPoolName, backendPoolParams, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to start extra backend address pool creation: %w", err)
		}
		backendPoolResp, err := backendPoolPoller.PollUntilDone(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create extra backend address pool: %w", err)
		}
		newBackendPoolID = backendPoolResp.ID
	}

	// Add both backend pools to the VMSS
	var backendPools []*armcompute.SubResource
	if existingBackendPoolID != nil {
		backendPools = append(backendPools, &armcompute.SubResource{ID: existingBackendPoolID})
	}
	if newBackendPoolID != nil {
		backendPools = append(backendPools, &armcompute.SubResource{ID: newBackendPoolID})
	}
	if len(backendPools) == 0 {
		return nil, nil, fmt.Errorf("no backend pools found or created for VMSS")
	}

	var inboundNatPools []*armcompute.SubResource
	if lb.Properties != nil && lb.Properties.InboundNatPools != nil && len(lb.Properties.InboundNatPools) > 0 {
		inboundNatPools = []*armcompute.SubResource{{ID: lb.Properties.InboundNatPools[0].ID}}
	}
	return backendPools, inboundNatPools, nil
}

// getSSHKey reads the SSH public key from the given path
func getSSHKey(sshKeyPath string) (string, error) {
	if sshKeyPath == "" {
		sshKeyPath = os.ExpandEnv("$HOME/.ssh/id_rsa.pub")
	}
	sshKeyBytes, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read SSH public key from %s: %w", sshKeyPath, err)
	}
	return string(sshKeyBytes), nil
}

// getPublicIP fetches the external IP address for the given public IP resource
func getPublicIP(ctx context.Context, subscriptionID, cluster, publicIPName string, cred *azidentity.DefaultAzureCredential) (string, error) {
	publicIPClient, err := armnetwork.NewPublicIPAddressesClient(subscriptionID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create public IP client: %w", err)
	}
	publicIPResp, err := publicIPClient.Get(ctx, cluster, publicIPName, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get public IP '%s': %w", publicIPName, err)
	}
	if publicIPResp.PublicIPAddress.Properties != nil && publicIPResp.PublicIPAddress.Properties.IPAddress != nil {
		return *publicIPResp.PublicIPAddress.Properties.IPAddress, nil
	}
	return "", fmt.Errorf("could not determine external IP for public IP resource '%s'", publicIPName)
}

// determineNodeType determines the kubeadm node type based on existing cluster state
func determineNodeType(ctx context.Context, role string, subscriptionID, cluster, keyVaultName string, cred *azidentity.DefaultAzureCredential) (string, error) {
	if role != "control-plane" {
		return "worker", nil
	}

	// Check if there's already a control-plane pool
	vmssClient, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create VMSS client: %w", err)
	}

	hasExistingControlPlane := false
	pager := vmssClient.NewListPager(cluster, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to list VMSS: %w", err)
		}
		for _, existingVMSS := range page.Value {
			if existingVMSS.Tags != nil {
				if v, ok := existingVMSS.Tags["k3a"]; ok && v != nil && *v == "control-plane" {
					hasExistingControlPlane = true
					break
				}
			}
		}
		if hasExistingControlPlane {
			break
		}
	}

	if !hasExistingControlPlane {
		return "first-master", nil
	}

	// Check if existing cluster is healthy
	installer := NewKubeadmInstaller(subscriptionID, cluster, keyVaultName, nil, cred)
	if installer.validateExistingCluster(ctx) {
		return "master", nil
	}

	// Existing cluster is unhealthy, become first-master
	fmt.Println("Existing control-plane cluster is unhealthy, promoting to first-master")
	return "first-master", nil
}

// installKubeadmOnInstances installs kubeadm on all instances in a VMSS
func installKubeadmOnInstances(ctx context.Context, subscriptionID, cluster, vmssName, role string, expectedCount int, cred *azidentity.DefaultAzureCredential) error {
	fmt.Printf("Installing kubeadm on VMSS: %s (role: %s)\n", vmssName, role)

	// Create VMSS manager to get instance information
	vmssManager := NewVMSSManager(subscriptionID, cluster, cred)

	// Wait for all instances to be running
	instances, err := vmssManager.WaitForVMSSInstancesRunning(ctx, vmssName, expectedCount, 10*time.Minute)
	if err != nil {
		return err
	}

	clusterHash := kstrings.UniqueString(cluster)
	keyVaultName := fmt.Sprintf("k3akv%s", clusterHash)
	lbName := fmt.Sprintf("k3alb%s", clusterHash)

	// Get load balancer public IP for SSH access
	lbPublicIP, err := vmssManager.GetLoadBalancerPublicIP(ctx, lbName)
	if err != nil {
		return fmt.Errorf("failed to get load balancer public IP: %w", err)
	}

	// Get NAT port mappings for SSH access
	natPortMappings, err := vmssManager.GetVMSSNATPortMappings(ctx, vmssName, lbName)
	if err != nil {
		return fmt.Errorf("failed to get NAT port mappings: %w", err)
	}

	// Determine the actual node type for kubeadm based on cluster state
	nodeType, err := determineNodeType(ctx, role, subscriptionID, cluster, keyVaultName, cred)
	if err != nil {
		return fmt.Errorf("failed to determine node type: %w", err)
	}

	fmt.Printf("Determined node type: %s\n", nodeType)

	// For control-plane, we only install on the first instance initially
	// Additional instances will be handled separately if needed
	var instancesToProcess []VMInstance
	if role == "control-plane" && nodeType == "first-master" {
		// Only process the first instance for initial cluster bootstrap
		instancesToProcess = instances[:1]
	} else {
		// Process all instances
		instancesToProcess = instances
	}

	// Install kubeadm on each instance
	for i, instance := range instancesToProcess {
		natPort, exists := natPortMappings[instance.Name]
		if !exists {
			return fmt.Errorf("no NAT port mapping found for instance %s", instance.Name)
		}

		fmt.Printf("Installing kubeadm on instance %s (NAT port: %d)\n", instance.Name, natPort)

		// Create SSH connection via load balancer NAT
		sshClient, err := CreateSSHClientViaNAT(lbPublicIP, natPort, "azureuser", "")
		if err != nil {
			return fmt.Errorf("failed to create SSH connection to %s: %w", instance.Name, err)
		}
		defer sshClient.Close()

		// Create kubeadm installer
		installer := NewKubeadmInstaller(subscriptionID, cluster, keyVaultName, sshClient, cred)

		// Install based on node type
		switch nodeType {
		case "first-master":
			if err := installer.InstallAsFirstMaster(ctx); err != nil {
				return fmt.Errorf("failed to install first master on %s: %w", instance.Name, err)
			}
			// After first master is installed, remaining instances should join as additional masters
			if role == "control-plane" && i == 0 && len(instancesToProcess) > 1 {
				nodeType = "master"
			}
		case "master":
			if err := installer.InstallAsAdditionalMaster(ctx); err != nil {
				return fmt.Errorf("failed to install additional master on %s: %w", instance.Name, err)
			}
		case "worker":
			if err := installer.InstallAsWorker(ctx); err != nil {
				return fmt.Errorf("failed to install worker on %s: %w", instance.Name, err)
			}
		default:
			return fmt.Errorf("unknown node type: %s", nodeType)
		}

		fmt.Printf("Successfully installed kubeadm on instance %s\n", instance.Name)
	}

	// If we have more control-plane instances and we just created the first master,
	// install the remaining instances as additional masters
	if role == "control-plane" && nodeType == "first-master" && len(instances) > 1 {
		fmt.Printf("Installing additional control-plane instances (%d remaining)\n", len(instances)-1)

		for _, instance := range instances[1:] {
			natPort, exists := natPortMappings[instance.Name]
			if !exists {
				return fmt.Errorf("no NAT port mapping found for instance %s", instance.Name)
			}

			fmt.Printf("Installing kubeadm as additional master on instance %s (NAT port: %d)\n", instance.Name, natPort)

			// Create SSH connection via load balancer NAT
			sshClient, err := CreateSSHClientViaNAT(lbPublicIP, natPort, "azureuser", "")
			if err != nil {
				return fmt.Errorf("failed to create SSH connection to %s: %w", instance.Name, err)
			}
			defer sshClient.Close()

			// Create kubeadm installer
			installer := NewKubeadmInstaller(subscriptionID, cluster, keyVaultName, sshClient, cred)

			if err := installer.InstallAsAdditionalMaster(ctx); err != nil {
				return fmt.Errorf("failed to install additional master on %s: %w", instance.Name, err)
			}

			fmt.Printf("Successfully installed kubeadm as additional master on instance %s\n", instance.Name)
		}
	}

	fmt.Printf("Kubeadm installation completed successfully on all instances\n")
	return nil
}

func Create(args CreatePoolArgs) error {
	subscriptionID := args.SubscriptionID
	cluster := args.Cluster
	location := args.Location
	role := args.Role
	if role != "" && role != "control-plane" && role != "worker" {
		return fmt.Errorf("invalid role: %s (must be 'control-plane' or 'worker')", role)
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	ctx := context.Background()
	vmssClient, err := armcompute.NewVirtualMachineScaleSetsClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create VMSS client: %w", err)
	}
	vmssName := args.Name + "-vmss"
	vmss, err := vmssClient.Get(ctx, cluster, vmssName, nil)
	if err == nil && vmss.Name != nil {
		if vmss.Tags != nil {
			if v, ok := vmss.Tags["k3a"]; ok && v != nil {
				existingRole := *v
				if existingRole != role {
					return fmt.Errorf("VMSS '%s' already exists with a different role: %s", vmssName, existingRole)
				}
			}
		}
	}

	if role == "control-plane" {
		pager := vmssClient.NewListPager(cluster, nil)
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed to list VMSS: %w", err)
			}
			for _, existingVMSS := range page.Value {
				if existingVMSS.Name != nil && *existingVMSS.Name != vmssName && existingVMSS.Tags != nil {
					if v, ok := existingVMSS.Tags["k3a"]; ok && v != nil && *v == "control-plane" {
						return fmt.Errorf("a VMSS with role 'control-plane' already exists: %s", *existingVMSS.Name)
					}
				}
			}
		}
	}

	sshKey, err := getSSHKey(args.SSHKeyPath)
	if err != nil {
		return err
	}

	clusterHash := kstrings.UniqueString(cluster)

	publicIPName := fmt.Sprintf("k3alb%s-publicip", clusterHash)
	externalIP, err := getPublicIP(ctx, subscriptionID, cluster, publicIPName, cred)
	if err != nil {
		return err
	}

	// Reference existing resources
	msi, err := getManagedIdentity(ctx, subscriptionID, cluster, cred)
	if err != nil {
		return err
	}

	// Collect all MSIs: default + user-specified
	userAssignedIdentities := map[string]*armcompute.VirtualMachineScaleSetIdentityUserAssignedIdentitiesValue{
		*msi.ID: {},
	}
	for _, id := range args.MSIIDs {
		userAssignedIdentities[id] = &armcompute.VirtualMachineScaleSetIdentityUserAssignedIdentitiesValue{}
	}

	keyVaultName := fmt.Sprintf("k3akv%s", clusterHash)
	storageAccountName := fmt.Sprintf("k3astorage%s", clusterHash)
	tmplData := map[string]string{
		"KeyVaultName":       keyVaultName,
		"Role":               role,
		"StorageAccountName": storageAccountName,
		"ResourceGroup":      cluster,
		"ExternalIP":         externalIP,
		"K8sVersion":         args.K8sVersion, // Pass version to template
		"MSIClientID":        *msi.Properties.ClientID,
	}

	customDataB64, err := getCloudInitData(tmplData)
	if err != nil {
		return err
	}

	isControlPlane := false
	if role == "control-plane" {
		isControlPlane = true
	}

	instanceCount := args.InstanceCount

	vnetName := "k3a-vnet"

	subnet, err := getSubnet(ctx, subscriptionID, cluster, vnetName, cred)
	if err != nil {
		return err
	}

	// Prepare VMSS parameters
	var backendPools []*armcompute.SubResource
	var inboundNatPools []*armcompute.SubResource
	lbName := fmt.Sprintf("k3alb%s", clusterHash)
	backendPools, inboundNatPools, err = getLoadBalancerPools(ctx, subscriptionID, cluster, lbName, args.Name, cred)
	if err != nil {
		return err
	}

	if !isControlPlane {
		inboundNatPools = nil
	}

	storageProfile := &armcompute.VirtualMachineScaleSetStorageProfile{
		ImageReference: &armcompute.ImageReference{
			Publisher: to.Ptr("MicrosoftCblMariner"),
			Offer:     to.Ptr("Cbl-Mariner"),
			SKU:       to.Ptr("cbl-mariner-2-gen2"),
			Version:   to.Ptr("latest"),
		},
		OSDisk: &armcompute.VirtualMachineScaleSetOSDisk{
			CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
			ManagedDisk: &armcompute.VirtualMachineScaleSetManagedDiskParameters{
				StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS),
			},
			DiskSizeGB: to.Ptr(int32(args.OSDiskSizeGB)),
		},
	}

	vmssParams := armcompute.VirtualMachineScaleSet{
		Location: to.Ptr(location),
		SKU: &armcompute.SKU{
			Name:     to.Ptr(args.SKU),
			Tier:     to.Ptr("Standard"),
			Capacity: to.Ptr[int64](int64(instanceCount)),
		},
		Tags: map[string]*string{
			"k3a": to.Ptr(role),
		},
		Identity: &armcompute.VirtualMachineScaleSetIdentity{
			Type:                   to.Ptr(armcompute.ResourceIdentityTypeUserAssigned),
			UserAssignedIdentities: userAssignedIdentities,
		},
		Properties: &armcompute.VirtualMachineScaleSetProperties{
			UpgradePolicy: &armcompute.UpgradePolicy{
				Mode: to.Ptr(armcompute.UpgradeModeManual),
			},
			VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
				OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{
					ComputerNamePrefix: to.Ptr(fmt.Sprintf("%s-", args.Name)),
					AdminUsername:      to.Ptr("azureuser"),
					CustomData:         to.Ptr(customDataB64),
					LinuxConfiguration: &armcompute.LinuxConfiguration{
						DisablePasswordAuthentication: to.Ptr(true),
						SSH: &armcompute.SSHConfiguration{
							PublicKeys: []*armcompute.SSHPublicKey{
								{
									Path:    to.Ptr("/home/azureuser/.ssh/authorized_keys"),
									KeyData: to.Ptr(sshKey),
								},
							},
						},
					},
				},
				StorageProfile: storageProfile,
				NetworkProfile: &armcompute.VirtualMachineScaleSetNetworkProfile{
					NetworkInterfaceConfigurations: []*armcompute.VirtualMachineScaleSetNetworkConfiguration{
						{
							Name: to.Ptr(args.Name + "-nic"),
							Properties: &armcompute.VirtualMachineScaleSetNetworkConfigurationProperties{
								Primary: to.Ptr(true),
								IPConfigurations: []*armcompute.VirtualMachineScaleSetIPConfiguration{
									{
										Name: to.Ptr(args.Name + "-ipconfig"),
										Properties: &armcompute.VirtualMachineScaleSetIPConfigurationProperties{
											Subnet: &armcompute.APIEntityReference{
												ID: subnet.ID,
											},
											LoadBalancerBackendAddressPools: backendPools,
											LoadBalancerInboundNatPools:     inboundNatPools,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	poller, err := vmssClient.BeginCreateOrUpdate(ctx, cluster, vmssName, vmssParams, nil)
	if err != nil {
		return fmt.Errorf("failed to start VMSS creation: %w", err)
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return fmt.Errorf("VMSS creation failed: %w", err)
	}

	if args.Role == "control-plane" {
		if err := rule.Create(rule.CreateRuleArgs{
			SubscriptionID: subscriptionID,
			ResourceGroup:  cluster,
			LBName:         lbName,
			RuleName:       "kubernetes-api",
			FrontendPort:   6443,
			BackendPort:    6443,
		}); err != nil {
			return fmt.Errorf("failed to create kubernetes API load balancing rule: %w", err)
		}
	}

	fmt.Printf("VMSS deployment succeeded: %v\n", *resp.ID)

	// Install kubeadm on the newly created instances
	if err := installKubeadmOnInstances(ctx, subscriptionID, cluster, args.Name+"-vmss", args.Role, args.InstanceCount, cred); err != nil {
		return fmt.Errorf("kubeadm installation failed: %w", err)
	}

	return nil
}
