param location string
param vnetNamePrefix string
param msiPrincipalId string
param clusterHash string


var storageUnique = clusterHash

resource storage 'Microsoft.Storage/storageAccounts@2023-01-01' = {
  name: toLower('${vnetNamePrefix}storage${storageUnique}')
  location: location
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
  properties: {
    accessTier: 'Hot'
  }
}

resource blobContributorRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(storage.id, msiPrincipalId, 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
  scope: storage
  properties: {
    principalId: msiPrincipalId
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
    principalType: 'ServicePrincipal'
  }
}
