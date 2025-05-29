param location string
param vnetNamePrefix string
param clusterHash string

var lbName = toLower('${vnetNamePrefix}lb${clusterHash}')

var internalFrontendIPConfig = {
  name: 'InternalLoadBalancerFrontend'
  properties: {
    privateIPAllocationMethod: 'Dynamic'
    subnet: {
      id: resourceId('Microsoft.Network/virtualNetworks/subnets', '${vnetNamePrefix}-vnet', 'default')
    }
  }
}

resource loadBalancer 'Microsoft.Network/loadBalancers@2022-09-01' = {
  name: lbName
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    frontendIPConfigurations: [
      {
        name: 'LoadBalancerFrontend'
        properties: {
          publicIPAddress: {
            id: publicIP.id
          }
        }
      }
    ]
    backendAddressPools: [
      {
        name: 'BackendPool'
      }
    ]
    inboundNatPools: [
      {
        name: 'ssh'
        properties: {
          frontendIPConfiguration: {
            id: resourceId('Microsoft.Network/loadBalancers/frontendIPConfigurations', lbName, 'LoadBalancerFrontend')
          }
          protocol: 'Tcp'
          frontendPortRangeStart: 50000
          frontendPortRangeEnd: 50100
          backendPort: 22
        }
      }
    ]
    outboundRules: [
      {
        name: 'OutboundRule'
        properties: {
          backendAddressPool: {
            id: resourceId('Microsoft.Network/loadBalancers/backendAddressPools', lbName, 'BackendPool')
          }
          frontendIPConfigurations: [
            {
              id: resourceId('Microsoft.Network/loadBalancers/frontendIPConfigurations', lbName, 'LoadBalancerFrontend')
            }
          ]
          protocol: 'All'
          allocatedOutboundPorts: 1000
        }
      }
    ]
  }
}

resource publicIP 'Microsoft.Network/publicIPAddresses@2022-09-01' = {
  name: '${lbName}-publicIP'
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    publicIPAllocationMethod: 'Static'
  }
}

resource privateDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' = {
  name: 'cluster.internal'
  location: 'global'
}

resource privateDnsZoneVnetLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  name: 'kubernetes-internal-link'
  parent: privateDnsZone
  location: 'global'
  properties: {
    virtualNetwork: {
      id: resourceId('Microsoft.Network/virtualNetworks', '${vnetNamePrefix}-vnet')
    }
    registrationEnabled: false
  }
}

resource msi 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' existing = {
  name: '${vnetNamePrefix}-msi'
}

resource dnsZoneContributorRoleDefinition 'Microsoft.Authorization/roleDefinitions@2022-04-01' existing = {
  scope: subscription()
  name: 'b12aa53e-6015-4669-85d0-8515ebb3ae7f' // Private DNS Zone Contributor role ID
}

resource privateDnsRoleAssignment 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(privateDnsZone.id, msi.id, dnsZoneContributorRoleDefinition.id)
  scope: privateDnsZone
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b12aa53e-6015-4669-85d0-8515ebb3ae7f')
    principalId: msi.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

output loadBalancerName string = loadBalancer.name
output publicIPAddress string = publicIP.properties.ipAddress
