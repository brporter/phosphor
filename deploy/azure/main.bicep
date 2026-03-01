// Phosphor relay server — Azure Container Apps infrastructure
// Deploys: ACR, managed identity, Log Analytics, Container Apps Environment,
// Container App, and DNS records for custom domain verification.

@description('Name prefix for all resources')
param name string = 'phosphor'

@description('Azure region')
param location string = resourceGroup().location

@description('Container image to deploy (use placeholder for initial provisioning)')
param containerImage string = 'mcr.microsoft.com/k8se/quickstart:latest'

@description('Custom domain hostname')
param customDomain string = 'phosphor.betaporter.dev'

@description('Existing DNS zone name in this resource group')
param dnsZoneName string = 'betaporter.dev'

@description('DNS subdomain record name')
param dnsRecordName string = 'phosphor'

// OIDC provider credentials (all optional — provider is registered only when set)
@secure()
param microsoftClientId string = ''
@secure()
param microsoftClientSecret string = ''
@secure()
param googleClientId string = ''
@secure()
param googleClientSecret string = ''
@secure()
param appleClientId string = ''
@secure()
param appleTeamId string = ''
@secure()
param appleKeyId string = ''
@secure()
param applePrivateKey string = ''

// Globally unique ACR name (alphanumeric, 5-50 chars)
var acrName = '${name}${uniqueString(resourceGroup().id)}'

// ---------- Azure Container Registry ----------

resource acr 'Microsoft.ContainerRegistry/registries@2023-07-01' = {
  name: acrName
  location: location
  sku: {
    name: 'Basic'
  }
  properties: {
    adminUserEnabled: false
  }
}

// ---------- Managed Identity for ACR pull ----------

resource managedIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: '${name}-id'
  location: location
}

// AcrPull role: 7f951dda-4ed3-4680-a7ca-43fe172d538d
resource acrPullRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(acr.id, managedIdentity.id, '7f951dda-4ed3-4680-a7ca-43fe172d538d')
  scope: acr
  properties: {
    roleDefinitionId: subscriptionResourceId(
      'Microsoft.Authorization/roleDefinitions',
      '7f951dda-4ed3-4680-a7ca-43fe172d538d'
    )
    principalId: managedIdentity.properties.principalId
    principalType: 'ServicePrincipal'
  }
}

// ---------- Log Analytics ----------

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: '${name}-logs'
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: 30
  }
}

// ---------- Container Apps Environment ----------

resource containerAppEnv 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: '${name}-env'
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalytics.properties.customerId
        sharedKey: logAnalytics.listKeys().primarySharedKey
      }
    }
  }
}

// ---------- Container App ----------

resource containerApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${managedIdentity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerAppEnv.id
    configuration: {
      registries: [
        {
          server: acr.properties.loginServer
          identity: managedIdentity.id
        }
      ]
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
      }
    }
    template: {
      containers: [
        {
          name: 'relay'
          image: containerImage
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          env: [
            { name: 'ADDR', value: ':8080' }
            { name: 'BASE_URL', value: 'https://${customDomain}' }
            { name: 'MICROSOFT_CLIENT_ID', value: microsoftClientId }
            { name: 'MICROSOFT_CLIENT_SECRET', value: microsoftClientSecret }
            { name: 'GOOGLE_CLIENT_ID', value: googleClientId }
            { name: 'GOOGLE_CLIENT_SECRET', value: googleClientSecret }
            { name: 'APPLE_CLIENT_ID', value: appleClientId }
            { name: 'APPLE_TEAM_ID', value: appleTeamId }
            { name: 'APPLE_KEY_ID', value: appleKeyId }
            { name: 'APPLE_PRIVATE_KEY', value: applePrivateKey }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 3
        rules: [
          {
            name: 'http-scale'
            http: {
              metadata: {
                concurrentRequests: '50'
              }
            }
          }
        ]
      }
    }
  }
}

// ---------- DNS records for custom domain ----------

resource dnsZone 'Microsoft.Network/dnsZones@2018-05-01' existing = {
  name: dnsZoneName
}

// CNAME: phosphor.betaporter.dev -> container app default FQDN
resource cnameRecord 'Microsoft.Network/dnsZones/CNAME@2018-05-01' = {
  parent: dnsZone
  name: dnsRecordName
  properties: {
    TTL: 300
    CNAMERecord: {
      cname: containerApp.properties.configuration.ingress.fqdn
    }
  }
}

// TXT: asuid.phosphor -> environment verification ID (required for custom domain binding)
resource txtRecord 'Microsoft.Network/dnsZones/TXT@2018-05-01' = {
  parent: dnsZone
  name: 'asuid.${dnsRecordName}'
  properties: {
    TTL: 300
    TXTRecords: [
      {
        value: [containerAppEnv.properties.customDomainConfiguration.customDomainVerificationId]
      }
    ]
  }
}

// ---------- Outputs ----------

output fqdn string = containerApp.properties.configuration.ingress.fqdn
output customDomainUrl string = 'https://${customDomain}'
output acrLoginServer string = acr.properties.loginServer
output acrName string = acr.name
output verificationId string = containerAppEnv.properties.customDomainConfiguration.customDomainVerificationId
output managedIdentityClientId string = managedIdentity.properties.clientId
