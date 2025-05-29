targetScope = 'subscription'

param resourceGroupName string
param location string
param vnetAddressSpace array
param vnetNamePrefix string
param clusterHash string
param callingPrincipalId string

resource rg 'Microsoft.Resources/resourceGroups@2022-09-01' = {
  name: resourceGroupName
  location: location
  tags: {
    k3a: 'cluster'
  }
}

module vnetModule 'modules/vnet.bicep' = {
  name: 'vnetDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetAddressSpace: vnetAddressSpace
    vnetNamePrefix: vnetNamePrefix
  }
  dependsOn: [
    rg
  ]
}

module identityModule 'modules/identity.bicep' = {
  name: 'identityDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetNamePrefix: vnetNamePrefix
  }
  dependsOn: [
    rg
  ]
}

module storageModule 'modules/storage.bicep' = {
  name: 'storageDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetNamePrefix: vnetNamePrefix
    msiPrincipalId: identityModule.outputs.principalId
    clusterHash: clusterHash
  }
}

module keyVaultModule 'modules/keyvault.bicep' = {
  name: 'keyVaultDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetNamePrefix: vnetNamePrefix
    msiPrincipalId: identityModule.outputs.principalId
    clusterHash: clusterHash
    callingPrincipalId: callingPrincipalId
  }
  dependsOn: [
    rg
  ]
}

param adminUsername string
@secure()
param adminPassword string

module postgresModule 'modules/postgres.bicep' = {
  name: 'postgresDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetNamePrefix: vnetNamePrefix
    adminUsername: adminUsername
    adminPassword: adminPassword
    clusterHash: clusterHash
  }
  dependsOn: [
    rg
  ]
}

module loadBalancerModule 'modules/loadbalancer.bicep' = {
  name: 'loadBalancerDeployment'
  scope: resourceGroup(resourceGroupName)
  params: {
    location: location
    vnetNamePrefix: vnetNamePrefix
    clusterHash: clusterHash
  }
  dependsOn: [
    rg
  ]
}
