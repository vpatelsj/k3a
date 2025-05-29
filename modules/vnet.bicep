param location string
param vnetAddressSpace array
param vnetNamePrefix string

resource nsg 'Microsoft.Network/networkSecurityGroups@2023-02-01' = {
  name: '${vnetNamePrefix}-nsg'
  location: location
  properties: {
    securityRules: [
      {
        name: 'AllowCorpNet'
        properties: {
          priority: 100
          direction: 'Inbound'
          access: 'Allow'
          protocol: '*'
          sourcePortRange: '*'
          destinationPortRange: '*'
          sourceAddressPrefix: 'CorpNetPublic'
          destinationAddressPrefix: '*'
          description: 'Allow all ports from CorpNetPublic service tag'
        }
      }
    ]
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2023-02-01' = {
  name: '${vnetNamePrefix}-vnet'
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: vnetAddressSpace
    }
    subnets: [
      {
        name: 'default'
        properties: {
          addressPrefix: '10.1.0.0/16'
          networkSecurityGroup: {
            id: nsg.id
          }
        }
      }
      {
        name: 'postgres'
        properties: {
          addressPrefix: '10.2.0.0/24'
          networkSecurityGroup: {
            id: nsg.id
          }
          delegations: [
            {
              name: 'postgres-delegation'
              properties: {
                serviceName: 'Microsoft.DBforPostgreSQL/flexibleServers'
              }
            }
          ]
        }
      }
    ]
  }
}
