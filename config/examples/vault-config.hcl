# HashiCorp Vault configuration example for chat platform

# Enable KV secrets engine
path "secret/chat-platform/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

# Database secrets
path "secret/chat-platform/database" {
  capabilities = ["read"]
}

# Redis secrets  
path "secret/chat-platform/redis" {
  capabilities = ["read"]
}

# JWT secrets
path "secret/chat-platform/jwt" {
  capabilities = ["read"]
}

# Example Vault CLI commands to set up secrets:
# 
# # Enable KV secrets engine
# vault secrets enable -path=secret kv-v2
# 
# # Set database secrets
# vault kv put secret/chat-platform/database \
#   username="chatuser" \
#   password="secure-db-password" \
#   host="postgres-cluster.database.svc.cluster.local" \
#   port="5432" \
#   database="chatplatform"
# 
# # Set Redis secrets
# vault kv put secret/chat-platform/redis \
#   password="secure-redis-password" \
#   addresses="redis-cluster-0:6379,redis-cluster-1:6379,redis-cluster-2:6379"
# 
# # Set JWT secret
# vault kv put secret/chat-platform/jwt \
#   secret="your-super-secure-jwt-secret-key-here"
# 
# # Set service configuration
# vault kv put secret/chat-platform/services \
#   gateway_port="8080" \
#   chat_port="8081" \
#   presence_port="8082" \
#   call_port="8083" \
#   rate_limit="5000"

# Kubernetes authentication method configuration
# vault auth enable kubernetes
# vault write auth/kubernetes/config \
#   token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
#   kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443" \
#   kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

# Create a role for the chat platform
# vault write auth/kubernetes/role/chat-platform \
#   bound_service_account_names=chat-platform \
#   bound_service_account_namespaces=chat-platform \
#   policies=chat-platform-policy \
#   ttl=24h