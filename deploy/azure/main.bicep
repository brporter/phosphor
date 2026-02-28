@description('The name prefix for all resources')
param name string = 'phosphor'

@description('The Azure region for deployment')
param location string = resourceGroup().location

@description('Container image to deploy')
param containerImage string

@description('Microsoft OIDC client ID')
@secure()
param microsoftClientId string = ''

@description('Microsoft OIDC client secret')
@secure()
param microsoftClientSecret string = ''

@description('Google OIDC client ID')
@secure()
param googleClientId string = ''

@description('Google OIDC client secret')
@secure()
param googleClientSecret string = ''

// Log Analytics workspace for Container Apps
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

// Container Apps Environment
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

// Container App
resource containerApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: name
  location: location
  properties: {
    managedEnvironmentId: containerAppEnv.id
    configuration: {
      ingress: {
        external: true
        targetPort: 8080
        transport: 'auto'
        corsPolicy: {
          allowedOrigins: ['*']
        }
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
            { name: 'BASE_URL', value: 'https://${name}.${containerAppEnv.properties.defaultDomain}' }
            { name: 'MICROSOFT_CLIENT_ID', value: microsoftClientId }
            { name: 'MICROSOFT_CLIENT_SECRET', value: microsoftClientSecret }
            { name: 'GOOGLE_CLIENT_ID', value: googleClientId }
            { name: 'GOOGLE_CLIENT_SECRET', value: googleClientSecret }
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

output fqdn string = containerApp.properties.configuration.ingress.fqdn
output appUrl string = 'https://${containerApp.properties.configuration.ingress.fqdn}'
