param location string
param vnetNamePrefix string
param adminUsername string
@secure()
param adminPassword string
@minLength(3)
param clusterHash string

// Ensure the server name is a single segment and adheres to naming conventions
var postgresServerName = toLower('${vnetNamePrefix}pg${clusterHash}')

resource postgres 'Microsoft.DBforPostgreSQL/flexibleServers@2022-12-01' = {
  name: postgresServerName
  location: location
  properties: {
    administratorLogin: adminUsername
    administratorLoginPassword: adminPassword
    version: '15'

    authConfig: {
      activeDirectoryAuth: 'Enabled'
      passwordAuth: 'Enabled'
    }
    storage: {
      storageSizeGB: 32
    }
    highAvailability: {
      mode: 'Disabled'
    }
    backup: {
      backupRetentionDays: 7
    }
    network: {
      delegatedSubnetResourceId: resourceId(
        'Microsoft.Network/virtualNetworks/subnets',
        '${vnetNamePrefix}-vnet',
        'postgres'
      )
      privateDnsZoneArmResourceId: postgresPrivateDnsZone.id
    }
  }
  sku: {
    name: 'Standard_D2s_v3'
    tier: 'GeneralPurpose'
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2022-09-01' existing = {
  name: '${vnetNamePrefix}-vnet'
}

resource postgresPrivateDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' = {
  name: 'privatelink.postgres.database.azure.com'
  location: 'global'
}

resource dnsZoneVnetLink 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = {
  name: 'postgres-link'
  parent: postgresPrivateDnsZone
  location: 'global'
  properties: {
    virtualNetwork: {
      id: vnet.id
    }
    registrationEnabled: false
  }
}

output postgresServerName string = postgres.name
