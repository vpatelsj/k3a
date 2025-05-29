param location string
param vnetNamePrefix string

resource msi 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: '${vnetNamePrefix}-msi'
  location: location
}

output principalId string = msi.properties.principalId
