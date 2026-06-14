$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ConnectUrl = if ($env:CONNECT_URL) { $env:CONNECT_URL } else { "http://localhost:8083" }
$ConnectorName = if ($env:DEBEZIUM_CONNECTOR_NAME) { $env:DEBEZIUM_CONNECTOR_NAME } else { "postgres-connector" }
$ConfigFile = if ($env:CONFIG_FILE) { $env:CONFIG_FILE } else { Join-Path $ScriptDir "postgres-connector.json" }

try {
    Invoke-RestMethod `
        -Method Put `
        -Uri "$ConnectUrl/connectors/$ConnectorName/config" `
        -ContentType "application/json" `
        -InFile $ConfigFile | Out-Null

    Write-Host "Connector $ConnectorName registered via $ConnectUrl"
    exit 0
}
catch {
    Write-Warning "Direct Kafka Connect REST call failed, retrying from kafka-connect container..."
}

$ContainerConfig = "/tmp/$ConnectorName.json"
$DataBinaryArg = "@$ContainerConfig"

docker compose cp $ConfigFile "kafka-connect:$ContainerConfig"
docker compose exec -T kafka-connect curl -fsS `
    -X PUT `
    -H "Content-Type: application/json" `
    --data-binary $DataBinaryArg `
    -o /dev/null `
    "http://localhost:8083/connectors/$ConnectorName/config"

Write-Host "Connector $ConnectorName registered from kafka-connect container"
