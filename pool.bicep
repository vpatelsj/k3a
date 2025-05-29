param location string
param prefix string
param poolName string
param clusterHash string
param isControlPlane bool = false

@description('Whether to assign a NAT pool to the VMSS.')
param role string = ''
param sshPublicKey string = ''

@description('Base64-encoded customData for VMSS.')
param customData string

@description('Number of VMSS instances to deploy.')
param instanceCount int = 1

var vmssName = '${poolName}-vmss'
var lbName = toLower('${prefix}lb${clusterHash}')

resource msi 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' existing = {
  name: '${prefix}-msi'
}

resource vnet 'Microsoft.Network/virtualNetworks@2022-09-01' existing = {
  name: '${prefix}-vnet'
}

resource subnet 'Microsoft.Network/virtualNetworks/subnets@2022-09-01' existing = {
  parent: vnet
  name: 'default'
}

resource loadBalancer 'Microsoft.Network/loadBalancers@2022-09-01' existing = {
  name: lbName
}

resource vmss 'Microsoft.Compute/virtualMachineScaleSets@2022-11-01' = {
  name: vmssName
  location: location
  sku: {
    name: 'Standard_D2s_v3'
    tier: 'Standard'
    capacity: instanceCount
  }
  tags: {
    k3a: role
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${msi.id}': {}
    }
  }
  properties: {
    upgradePolicy: {
      mode: 'Manual'
    }
    virtualMachineProfile: {
      networkProfile: {
        networkInterfaceConfigurations: [
          {
            name: '${poolName}-nic'
            properties: {
              primary: true
              enableIPForwarding: true
              ipConfigurations: [
                {
                  name: 'ipconfig1'
                  properties: {
                    subnet: {
                      id: subnet.id
                    }
                    loadBalancerBackendAddressPools: isControlPlane ? [
                      {
                        id: loadBalancer.properties.backendAddressPools[0].id
                      }
                    ] : [
                      {
                        id: loadBalancer.properties.backendAddressPools[0].id
                      }
                    ]
                    loadBalancerInboundNatPools: isControlPlane ? [
                      {
                        id: loadBalancer.properties.inboundNatPools[0].id
                      }
                    ] : []
                  }
                }
              ]
            }
          }
        ]
      }
      storageProfile: {
        imageReference: {
          publisher: 'MicrosoftCblMariner'
          offer: 'Cbl-Mariner'
          sku: 'cbl-mariner-2-gen2'
          version: 'latest'
        }
        osDisk: {
          createOption: 'FromImage'
          managedDisk: {
            storageAccountType: 'Standard_LRS'
          }
        }
      }
      osProfile: {
        computerNamePrefix: '${poolName}-'
        adminUsername: 'azureuser'
        linuxConfiguration: {
          disablePasswordAuthentication: true
          ssh: {
            publicKeys: [
              {
                path: '/home/azureuser/.ssh/authorized_keys'
                keyData: sshPublicKey
              }
            ]
          }
        }
        customData: customData
      }
    }
  }
}

output vmssName string = vmss.name
