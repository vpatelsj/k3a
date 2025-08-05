#!/usr/bin/env bash

# Incremental Hollow Node Addition Script - Optimized with curl
# This script adds hollow nodes in batches using direct API calls for better performance

set -o nounset
set -o pipefail

# Configuration Variables
INITIAL_BATCH_SIZE="${INITIAL_BATCH_SIZE:-5}"  # Starting batch size
EXPONENTIAL_FACTOR="${EXPONENTIAL_FACTOR:-2}"  # Factor to multiply batch size each iteration
MAX_NODES="${MAX_NODES:-100}"  # Maximum total nodes to create
MAX_BATCHES="${MAX_BATCHES:-10}"  # Maximum number of batches as a safety limit
WAIT_TIMEOUT="${WAIT_TIMEOUT:-3000}"  # seconds to wait for nodes to become ready
KUBEMARK_IMAGE="${KUBEMARK_IMAGE:-acrvapa17.azurecr.io/kubemark:latest}"
PERFORMANCE_WAIT="${PERFORMANCE_WAIT:-30}"  # seconds to wait after batch before measuring performance
PERFORMANCE_TESTS="${PERFORMANCE_TESTS:-5}"  # number of API calls to test for performance measurement

# Test namespace
TEST_NAMESPACE="kubemark-incremental-test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m' # No Color

# Function to print colored output
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_batch() {
    echo -e "${BLUE}[BATCH]${NC} $1" >&2
}

log_success() {
    echo -e "${PURPLE}[SUCCESS]${NC} $1" >&2
}

log_perf() {
    echo -e "${BLUE}[PERF]${NC} $1" >&2
}

# Global variables for API access
declare -g API_SERVER=""
declare -g CLIENT_CERT=""
declare -g CLIENT_KEY=""
declare -g CA_CERT=""
declare -g AUTH_METHOD=""
declare -g AUTH_TOKEN=""

# Helper function to setup API access with cert auth
setup_api_access() {
    if [[ -z "${API_SERVER:-}" ]]; then
        API_SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
        
        if [[ -z "$API_SERVER" ]]; then
            log_error "Failed to get API server URL from kubeconfig"
            return 1
        fi
        
        log_info "API Server: $API_SERVER"
        
        # Check if we have client certificate data
        local cert_data=$(kubectl config view --minify --raw -o jsonpath='{.users[0].user.client-certificate-data}' 2>/dev/null)
        local key_data=$(kubectl config view --minify --raw -o jsonpath='{.users[0].user.client-key-data}' 2>/dev/null)
        local ca_data=$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' 2>/dev/null)
        
        if [[ -n "$cert_data" && -n "$key_data" && -n "$ca_data" ]]; then
            # Extract certificates
            echo "$cert_data" | base64 -d > /tmp/client.crt
            echo "$key_data" | base64 -d > /tmp/client.key
            echo "$ca_data" | base64 -d > /tmp/ca.crt
            
            CLIENT_CERT="/tmp/client.crt"
            CLIENT_KEY="/tmp/client.key"
            CA_CERT="/tmp/ca.crt"
            AUTH_METHOD="cert"
            
            log_info "Using client certificate authentication"
        else
            # Try token-based authentication
            local token=$(kubectl config view --minify --raw -o jsonpath='{.users[0].user.token}' 2>/dev/null)
            
            if [[ -z "$token" ]]; then
                # Try to get token from tokenFile
                local token_file=$(kubectl config view --minify --raw -o jsonpath='{.users[0].user.tokenFile}' 2>/dev/null)
                if [[ -n "$token_file" && -f "$token_file" ]]; then
                    token=$(cat "$token_file" 2>/dev/null)
                fi
            fi
            
            if [[ -n "$token" ]]; then
                AUTH_TOKEN="$token"
                AUTH_METHOD="token"
                
                # Still need CA cert for token auth
                if [[ -n "$ca_data" ]]; then
                    echo "$ca_data" | base64 -d > /tmp/ca.crt
                    CA_CERT="/tmp/ca.crt"
                fi
                
                log_info "Using token authentication"
            else
                log_error "No authentication method available (no client cert or token found)"
                return 1
            fi
        fi
    fi
}

# Function to make authenticated API calls
call_k8s_api() {
    local endpoint="$1"
    local method="${2:-GET}"
    local data="${3:-}"
    
    setup_api_access
    if [[ $? -ne 0 ]]; then
        return 1
    fi
    
    local curl_args=(-s --compressed)
    
    # Add authentication based on method
    if [[ "$AUTH_METHOD" == "cert" ]]; then
        curl_args+=(--cert "$CLIENT_CERT" --key "$CLIENT_KEY")
    elif [[ "$AUTH_METHOD" == "token" ]]; then
        curl_args+=(-H "Authorization: Bearer $AUTH_TOKEN")
    else
        log_error "Unknown authentication method: $AUTH_METHOD"
        return 1
    fi
    
    # Add CA cert if available
    if [[ -n "$CA_CERT" && -f "$CA_CERT" ]]; then
        curl_args+=(--cacert "$CA_CERT")
    else
        # Skip certificate verification as fallback
        curl_args+=(-k)
        log_warn "Using insecure connection (no CA certificate)"
    fi
    
    if [[ "$method" != "GET" ]]; then
        curl_args+=(-X "$method")
    fi
    
    if [[ -n "$data" ]]; then
        curl_args+=(-H "Content-Type: application/json" -d "$data")
    fi
    
    curl "${curl_args[@]}" "$API_SERVER$endpoint"
}

# Function to call API with table format for lighter payloads
call_k8s_api_table() {
    local endpoint="$1"
    setup_api_access
    if [[ $? -ne 0 ]]; then
        return 1
    fi
    
    local curl_args=(-s --compressed -H "Accept: application/json;as=Table;v=v1;g=meta.k8s.io")
    
    # Add authentication based on method
    if [[ "$AUTH_METHOD" == "cert" ]]; then
        curl_args+=(--cert "$CLIENT_CERT" --key "$CLIENT_KEY")
    elif [[ "$AUTH_METHOD" == "token" ]]; then
        curl_args+=(-H "Authorization: Bearer $AUTH_TOKEN")
    else
        log_error "Unknown authentication method: $AUTH_METHOD"
        return 1
    fi
    
    # Add CA cert if available
    if [[ -n "$CA_CERT" && -f "$CA_CERT" ]]; then
        curl_args+=(--cacert "$CA_CERT")
    else
        # Skip certificate verification as fallback
        curl_args+=(-k)
    fi
    
    local result=$(curl "${curl_args[@]}" "$API_SERVER$endpoint")
    
    # Debug: Check if we got an error response
    if echo "$result" | jq -e '.kind == "Status" and .status == "Failure"' >/dev/null 2>&1; then
        local error_msg=$(echo "$result" | jq -r '.message // "Unknown error"' 2>/dev/null)
        log_warn "Table format not supported: $error_msg"
        return 1
    fi
    
    echo "$result"
}

# Test function to verify API access
test_api_access() {
    log_info "Testing API access and table format support..."
    
    # First check if setup succeeded
    setup_api_access
    if [[ $? -ne 0 ]]; then
        log_error "❌ Failed to setup API access"
        return 1
    fi
    
    log_info "Authentication method: $AUTH_METHOD"
    
    # Test basic API access with debugging
    log_info "Testing /version endpoint..."
    local version_result=$(call_k8s_api "/version")
    local api_exit_code=$?
    
    if [[ $api_exit_code -ne 0 ]]; then
        log_error "❌ API call failed with exit code $api_exit_code"
        log_error "Trying kubectl version as fallback..."
        kubectl version --short 2>/dev/null || kubectl version 2>/dev/null
        return 1
    fi
    
    if [[ -z "$version_result" ]]; then
        log_error "❌ API call returned empty result"
        log_error "Debugging: curl exit code was $api_exit_code"
        return 1
    fi
    
    # Check if result is valid JSON
    if ! echo "$version_result" | jq . >/dev/null 2>&1; then
        log_error "❌ API call returned invalid JSON"
        log_error "Response: $version_result"
        return 1
    fi
    
    # Check if it's an error response
    local status_kind=$(echo "$version_result" | jq -r '.kind // ""' 2>/dev/null)
    if [[ "$status_kind" == "Status" ]]; then
        local error_msg=$(echo "$version_result" | jq -r '.message // "Unknown error"' 2>/dev/null)
        log_error "❌ API returned error: $error_msg"
        return 1
    fi
    
    local k8s_version=$(echo "$version_result" | jq -r '.gitVersion // "unknown"' 2>/dev/null)
    log_info "✅ API access working, Kubernetes version: $k8s_version"
    
    # Test table format
    local table_result=$(call_k8s_api_table "/api/v1/nodes?limit=1")
    if [[ $? -eq 0 && -n "$table_result" ]]; then
        local has_rows=$(echo "$table_result" | jq 'has("rows")' 2>/dev/null)
        if [[ "$has_rows" == "true" ]]; then
            log_info "✅ Table format supported"
        else
            log_warn "⚠️  Table format response doesn't have expected 'rows' field"
        fi
    else
        log_warn "⚠️  Table format not supported, will use regular API"
    fi
    
    # Test node listing
    local nodes_result=$(call_k8s_api "/api/v1/nodes?limit=1")
    if [[ $? -eq 0 && -n "$nodes_result" ]] && echo "$nodes_result" | jq . >/dev/null 2>&1; then
        local node_count=$(echo "$nodes_result" | jq '.items | length' 2>/dev/null || echo "0")
        log_info "✅ Node listing works, got $node_count nodes in test query"
    else
        log_error "❌ Node listing failed"
        return 1
    fi
    
    return 0
}

# Optimized node counting using API calls with pagination
get_kubemark_node_counts() {
    local start_time=$(date +%s.%3N 2>/dev/null || date +%s)
    local quiet_mode="${1:-false}"
    
    # Try table format first but with pagination for large clusters
    local total_registered=0
    local total_ready=0
    local continue_token=""
    local page_size=2000  # Optimized for large clusters (up to 50k nodes)
    local use_table_format=true
    local page_count=0
    local max_pages=100  # Safety limit to prevent infinite loops
    
    # Try one table format call first to see if it works
    local test_table_data=$(call_k8s_api_table "/api/v1/nodes?labelSelector=kubemark=true&limit=1")
    local test_exit_code=$?
    
    if [[ $test_exit_code -ne 0 ]] || ! echo "$test_table_data" | jq -e 'has("rows")' >/dev/null 2>&1; then
        use_table_format=false
        if [[ "$quiet_mode" != "true" ]]; then
            log_warn "Table format not available, using paginated regular API"
        fi
    fi
    
    if [[ "$use_table_format" == "true" ]]; then
        # Use paginated table format for better performance
        while [[ $page_count -lt $max_pages ]]; do
            local url="/api/v1/nodes?labelSelector=kubemark=true&limit=$page_size"
            if [[ -n "$continue_token" ]]; then
                url="${url}&continue=${continue_token}"
            fi
            
            local table_data=$(call_k8s_api_table "$url")
            local table_exit_code=$?
            
            if [[ $table_exit_code -ne 0 || -z "$table_data" ]]; then
                if [[ "$quiet_mode" != "true" ]]; then
                    log_warn "Table API call failed on page $((page_count + 1)), collected $total_registered nodes so far"
                fi
                break
            fi
            
            # Check if we got a valid table response
            local has_rows=$(echo "$table_data" | jq 'has("rows")' 2>/dev/null)
            if [[ "$has_rows" == "true" ]]; then
                # Process table format - typically: [name, status, roles, age, version]
                local page_counts=$(echo "$table_data" | jq '{
                    count: (.rows | length),
                    ready: ([.rows[] | select(.cells | length > 1) | select(.cells[1] == "Ready")] | length),
                    continue: (.metadata.continue // "")
                }' 2>/dev/null)
                
                if [[ $? -eq 0 && -n "$page_counts" ]]; then
                    local page_count_nodes=$(echo "$page_counts" | jq -r '.count' 2>/dev/null || echo "0")
                    local page_ready_nodes=$(echo "$page_counts" | jq -r '.ready' 2>/dev/null || echo "0")
                    continue_token=$(echo "$page_counts" | jq -r '.continue' 2>/dev/null || echo "")
                    
                    # Validate numbers before adding
                    if [[ "$page_count_nodes" =~ ^[0-9]+$ ]]; then
                        total_registered=$((total_registered + page_count_nodes))
                    fi
                    if [[ "$page_ready_nodes" =~ ^[0-9]+$ ]]; then
                        total_ready=$((total_ready + page_ready_nodes))
                    fi
                    
                    ((page_count++))
                    
                    # Debug info for large clusters
                    if [[ "$quiet_mode" != "true" && $page_count -gt 1 && $(($page_count % 10)) -eq 0 ]]; then
                        log_info "Processed page $page_count: $total_registered total nodes, $total_ready ready"
                    fi
                    
                    # Break if no more pages or no continue token
                    if [[ "$continue_token" == "null" || -z "$continue_token" || "$page_count_nodes" -eq 0 ]]; then
                        break
                    fi
                else
                    if [[ "$quiet_mode" != "true" ]]; then
                        log_warn "Failed to parse table response on page $((page_count + 1))"
                    fi
                    break
                fi
            else
                if [[ "$quiet_mode" != "true" ]]; then
                    log_warn "Invalid table response on page $((page_count + 1))"
                fi
                break
            fi
        done
        
        # Warn if we hit the page limit
        if [[ $page_count -ge $max_pages ]]; then
            if [[ "$quiet_mode" != "true" ]]; then
                log_warn "Hit maximum page limit ($max_pages), may have missed some nodes"
            fi
        fi
        
        # If we got valid results from table format, return them
        if [[ $total_registered -gt 0 ]]; then
            local end_time=$(date +%s.%3N 2>/dev/null || date +%s)
            local duration=""
            if [[ "$end_time" =~ ^[0-9.]+$ && "$start_time" =~ ^[0-9.]+$ ]]; then
                duration=$(echo "$end_time - $start_time" | bc 2>/dev/null || echo "N/A")
            else
                duration="N/A"
            fi
            if [[ "$quiet_mode" != "true" ]]; then
                log_info "Node count retrieved in ${duration}s via paginated table API ($page_count pages)"
            fi
            
            echo "$total_registered:$total_ready"
            return 0
        fi
    fi
    
    # Fallback to paginated regular API
    if [[ "$quiet_mode" != "true" ]]; then
        log_warn "Table format failed, falling back to paginated objects"
    fi
    total_registered=0
    total_ready=0
    continue_token=""
    page_size=2000  # Optimized for large clusters (up to 50k nodes)
    page_count=0
    max_pages=100  # Safety limit to prevent infinite loops
    
    while [[ $page_count -lt $max_pages ]]; do
        local url="/api/v1/nodes?labelSelector=kubemark=true&limit=$page_size"
        if [[ -n "$continue_token" ]]; then
            url="${url}&continue=${continue_token}"
        fi
        
        local page_data=$(call_k8s_api "$url")
        if [[ $? -ne 0 || -z "$page_data" ]]; then
            if [[ "$quiet_mode" != "true" ]]; then
                log_warn "Regular API call failed on page $((page_count + 1)), collected $total_registered nodes so far"
            fi
            break
        fi
        
        # Process this page immediately to reduce memory usage
        local page_results=$(echo "$page_data" | jq -c '{
            count: (.items | length),
            ready: (.items | map(select(.status.conditions[]? | select(.type=="Ready" and .status=="True"))) | length),
            continue: (.metadata.continue // "")
        }' 2>/dev/null)
        
        if [[ $? -ne 0 || -z "$page_results" ]]; then
            if [[ "$quiet_mode" != "true" ]]; then
                log_warn "Failed to parse regular API response on page $((page_count + 1))"
            fi
            break
        fi
        
        local page_count_nodes=$(echo "$page_results" | jq -r '.count' 2>/dev/null || echo "0")
        local page_ready_nodes=$(echo "$page_results" | jq -r '.ready' 2>/dev/null || echo "0")
        continue_token=$(echo "$page_results" | jq -r '.continue' 2>/dev/null || echo "")
        
        # Validate numbers before adding
        if [[ "$page_count_nodes" =~ ^[0-9]+$ ]]; then
            total_registered=$((total_registered + page_count_nodes))
        fi
        if [[ "$page_ready_nodes" =~ ^[0-9]+$ ]]; then
            total_ready=$((total_ready + page_ready_nodes))
        fi
        
        ((page_count++))
        
        # Debug info for large clusters
        if [[ "$quiet_mode" != "true" && $page_count -gt 1 && $(($page_count % 10)) -eq 0 ]]; then
            log_info "Processed page $page_count: $total_registered total nodes, $total_ready ready"
        fi
        
        # Break if no more pages or no continue token
        if [[ "$continue_token" == "null" || -z "$continue_token" || "$page_count_nodes" -eq 0 ]]; then
            break
        fi
    done
    
    # Warn if we hit the page limit
    if [[ $page_count -ge $max_pages ]]; then
        if [[ "$quiet_mode" != "true" ]]; then
            log_warn "Hit maximum page limit ($max_pages), may have missed some nodes"
        fi
    fi
    
    # If API calls failed completely, try kubectl as final fallback
    if [[ $total_registered -eq 0 ]]; then
        if [[ "$quiet_mode" != "true" ]]; then
            log_warn "API calls failed, using kubectl fallback"
        fi
        local registered_count=$(kubectl get nodes -l kubemark=true --output=name --no-headers 2>/dev/null | wc -l)
        local ready_count=$(kubectl get nodes -l kubemark=true \
            --output=custom-columns=READY:.status.conditions[?@.type==\"Ready\"].status \
            --no-headers 2>/dev/null | grep -c "True")
        
        # Validate kubectl results
        if [[ "$registered_count" =~ ^[0-9]+$ && "$ready_count" =~ ^[0-9]+$ ]]; then
            total_registered=$registered_count
            total_ready=$ready_count
        fi
    fi
    
    local end_time=$(date +%s.%3N 2>/dev/null || date +%s)
    local duration=""
    if [[ "$end_time" =~ ^[0-9.]+$ && "$start_time" =~ ^[0-9.]+$ ]]; then
        duration=$(echo "$end_time - $start_time" | bc 2>/dev/null || echo "N/A")
    else
        duration="N/A"
    fi
    if [[ "$quiet_mode" != "true" ]]; then
        log_info "Node count retrieved in ${duration}s via paginated API ($page_count pages)"
    fi
    
    echo "$total_registered:$total_ready"
}

# Get node names for deletion using API
get_kubemark_node_names() {
    # Try table format first for node names
    local table_data=$(call_k8s_api_table "/api/v1/nodes?labelSelector=kubemark=true")
    
    if [[ $? -eq 0 && -n "$table_data" ]] && echo "$table_data" | jq . >/dev/null 2>&1; then
        local has_rows=$(echo "$table_data" | jq 'has("rows")' 2>/dev/null)
        if [[ "$has_rows" == "true" ]]; then
            local node_names=$(echo "$table_data" | jq -r '.rows[]? | select(.cells | length > 0) | .cells[0]' 2>/dev/null)
            if [[ -n "$node_names" ]]; then
                echo "$node_names"
                return 0
            fi
        fi
    fi
    
    # Fallback to regular API
    local nodes_json=$(call_k8s_api "/api/v1/nodes?labelSelector=kubemark=true")
    if [[ $? -eq 0 && -n "$nodes_json" ]] && echo "$nodes_json" | jq . >/dev/null 2>&1; then
        local node_names=$(echo "$nodes_json" | jq -r '.items[].metadata.name' 2>/dev/null)
        if [[ -n "$node_names" ]]; then
            echo "$node_names"
            return 0
        fi
    fi
    
    # Final fallback to kubectl
    kubectl get nodes -l kubemark=true --output=name --no-headers 2>/dev/null | sed 's/node\///'
}

# Function to measure comprehensive cluster performance with curl optimization
measure_cluster_performance() {
    local batch_number="$1"
    local total_nodes="$2"
    
    log_perf "Measuring comprehensive cluster performance after batch $batch_number ($total_nodes nodes)..."
    
    # Array to store response times
    local response_times=()
    local failed_requests=0
    
    # Collect system metrics using API calls when possible
    local start_metrics_time=$(date +%s.%3N 2>/dev/null || date +%s)
    
    # Get resource usage - fallback to kubectl for top commands
    local master_nodes_info=$(kubectl top nodes --no-headers 2>/dev/null | grep "k3s-master" || echo "")
    
    if [[ -n "$master_nodes_info" ]]; then
        # Calculate total and average CPU/Memory across all master nodes
        local total_cpu=0
        local total_memory=0
        local master_count=0
        
        while IFS= read -r line; do
            if [[ -n "$line" ]]; then
                local cpu_val=$(echo "$line" | awk '{print $2}' | sed 's/m//')
                local mem_val=$(echo "$line" | awk '{print $4}' | sed 's/Mi//')
                
                # Handle different units (cores vs millicores)
                if [[ "$cpu_val" =~ ^[0-9]+$ ]]; then
                    total_cpu=$((total_cpu + cpu_val))
                elif [[ "$cpu_val" =~ ^[0-9]+m$ ]]; then
                    cpu_val=$(echo "$cpu_val" | sed 's/m//')
                    if [[ "$cpu_val" =~ ^[0-9]+$ ]]; then
                        total_cpu=$((total_cpu + cpu_val))
                    fi
                elif [[ "$cpu_val" =~ ^[0-9.]+$ ]]; then
                    cpu_val=$(echo "$cpu_val * 1000" | bc -l 2>/dev/null | cut -d. -f1)
                    if [[ "$cpu_val" =~ ^[0-9]+$ ]]; then
                        total_cpu=$((total_cpu + cpu_val))
                    fi
                fi
                
                if [[ "$mem_val" =~ ^[0-9]+$ ]]; then
                    total_memory=$((total_memory + mem_val))
                elif [[ "$mem_val" =~ ^[0-9]+Mi$ ]]; then
                    mem_val=$(echo "$mem_val" | sed 's/Mi//')
                    if [[ "$mem_val" =~ ^[0-9]+$ ]]; then
                        total_memory=$((total_memory + mem_val))
                    fi
                elif [[ "$mem_val" =~ ^[0-9.]+Gi$ ]]; then
                    mem_val=$(echo "$mem_val" | sed 's/Gi//')
                    mem_val=$(echo "$mem_val * 1024" | bc -l 2>/dev/null | cut -d. -f1)
                    if [[ "$mem_val" =~ ^[0-9]+$ ]]; then
                        total_memory=$((total_memory + mem_val))
                    fi
                fi
                
                ((master_count++))
            fi
        done <<< "$master_nodes_info"
        
        local api_server_cpu=$total_cpu
        local api_server_memory=$total_memory
        local etcd_cpu=$(( total_cpu / 3 ))
        local etcd_memory=$(( total_memory / 4 ))
    else
        # Fallback for traditional K8s
        local api_server_cpu=$(kubectl top pods -n kube-system --no-headers 2>/dev/null | grep kube-apiserver | awk '{print $2}' | sed 's/m//' | head -1 || echo "0")
        local api_server_memory=$(kubectl top pods -n kube-system --no-headers 2>/dev/null | grep kube-apiserver | awk '{print $3}' | sed 's/Mi//' | head -1 || echo "0")
        local etcd_cpu=$(kubectl top pods -n kube-system --no-headers 2>/dev/null | grep etcd | awk '{print $2}' | sed 's/m//' | head -1 || echo "0")
        local etcd_memory=$(kubectl top pods -n kube-system --no-headers 2>/dev/null | grep etcd | awk '{print $3}' | sed 's/Mi//' | head -1 || echo "0")
    fi
    
    local end_metrics_time=$(date +%s.%3N 2>/dev/null || date +%s)
    local metrics_collection_time=""
    if [[ "$end_metrics_time" =~ ^[0-9.]+$ && "$start_metrics_time" =~ ^[0-9.]+$ ]]; then
        metrics_collection_time=$(echo "$end_metrics_time - $start_metrics_time" | bc -l 2>/dev/null || echo "N/A")
    else
        metrics_collection_time="N/A"
    fi
    
    # Count cluster objects using API calls where possible
    local total_pods_json=$(call_k8s_api "/api/v1/pods")
    local total_pods=$(echo "$total_pods_json" | jq '.items | length' 2>/dev/null || echo "0")
    
    local total_services_json=$(call_k8s_api "/api/v1/services")
    local total_services=$(echo "$total_services_json" | jq '.items | length' 2>/dev/null || echo "0")
    
    local total_deployments_json=$(call_k8s_api "/apis/apps/v1/deployments")
    local total_deployments=$(echo "$total_deployments_json" | jq '.items | length' 2>/dev/null || echo "0")
    
    local total_configmaps_json=$(call_k8s_api "/api/v1/configmaps")
    local total_configmaps=$(echo "$total_configmaps_json" | jq '.items | length' 2>/dev/null || echo "0")
    
    local total_secrets_json=$(call_k8s_api "/api/v1/secrets")
    local total_secrets=$(echo "$total_secrets_json" | jq '.items | length' 2>/dev/null || echo "0")
    
    # Test different API endpoints with detailed timing using curl
    local endpoints=(
        "/api/v1/nodes"
        "/api/v1/pods"
        "/api/v1/services"
        "/api/v1/nodes?labelSelector=kubemark=true"
        "/apis/apps/v1/deployments"
        "/api/v1/configmaps"
        "/api/v1/events?limit=100"
        "/version"
        "/api/v1/namespaces"
        "/api/v1"
    )
    
    local endpoint_names=(
        "get nodes"
        "get pods --all-namespaces"
        "get services --all-namespaces"
        "get nodes -l kubemark=true"
        "get deployments --all-namespaces"
        "get configmaps --all-namespaces"
        "get events --all-namespaces --limit=100"
        "version"
        "get namespaces"
        "api-resources"
    )
    
    # Endpoint-specific metrics
    declare -A endpoint_stats
    
    echo "  Testing API endpoints with curl:"
    for i in "${!endpoints[@]}"; do
        local endpoint="${endpoints[$i]}"
        local endpoint_name="${endpoint_names[$i]}"
        echo -n "    Testing: $endpoint_name ... "
        
        local endpoint_times=()
        local endpoint_failures=0
        local endpoint_key=$(echo "$endpoint_name" | tr ' ' '_' | tr '-' '_')
        
        # Run multiple tests for this endpoint
        for (( j=1; j<=PERFORMANCE_TESTS; j++ )); do
            local start_time=$(date +%s.%3N 2>/dev/null || date +%s)
            
            # Use table format for lighter payloads where applicable
            local result=""
            if [[ "$endpoint" == "/api/v1/nodes"* ]]; then
                result=$(call_k8s_api_table "$endpoint")
            else
                result=$(call_k8s_api "$endpoint")
            fi
            
            if [[ $? -eq 0 && -n "$result" ]] && echo "$result" | jq . >/dev/null 2>&1; then
                local end_time=$(date +%s.%3N 2>/dev/null || date +%s)
                if [[ "$end_time" =~ ^[0-9.]+$ && "$start_time" =~ ^[0-9.]+$ ]]; then
                    local response_time=$(echo "$end_time - $start_time" | bc -l 2>/dev/null)
                    if [[ "$response_time" =~ ^[0-9.]+$ ]]; then
                        endpoint_times+=($response_time)
                        response_times+=($response_time)
                    fi
                fi
            else
                ((endpoint_failures++))
                ((failed_requests++))
            fi
        done
        
        # Calculate statistics for this endpoint
        if [[ ${#endpoint_times[@]} -gt 0 ]]; then
            local sum=0
            local min=${endpoint_times[0]}
            local max=${endpoint_times[0]}
            
            for time in "${endpoint_times[@]}"; do
                if [[ "$time" =~ ^[0-9.]+$ ]]; then
                    sum=$(echo "$sum + $time" | bc -l 2>/dev/null || echo "$sum")
                    if [[ -n "$sum" ]] && (( $(echo "$time < $min" | bc -l 2>/dev/null || echo "0") )); then
                        min=$time
                    fi
                    if [[ -n "$sum" ]] && (( $(echo "$time > $max" | bc -l 2>/dev/null || echo "0") )); then
                        max=$time
                    fi
                fi
            done
            
            local avg=""
            if [[ "$sum" =~ ^[0-9.]+$ && ${#endpoint_times[@]} -gt 0 ]]; then
                avg=$(echo "scale=3; $sum / ${#endpoint_times[@]}" | bc -l 2>/dev/null || echo "N/A")
            else
                avg="N/A"
            fi
            endpoint_stats[$endpoint_key]="$avg,$min,$max,$endpoint_failures"
            
            if [[ "$avg" =~ ^[0-9.]+$ ]]; then
                printf "avg: %.3fs, min: %.3fs, max: %.3fs" "$avg" "$min" "$max"
            else
                printf "avg: %s, min: %s, max: %s" "$avg" "$min" "$max"
            fi
            
            if [[ $endpoint_failures -gt 0 ]]; then
                echo " (${endpoint_failures}/${PERFORMANCE_TESTS} failed)"
            else
                echo " ✓"
            fi
        else
            endpoint_stats[$endpoint_key]="N/A,N/A,N/A,$PERFORMANCE_TESTS"
            echo "ALL FAILED"
        fi
    done
    
    # Calculate overall statistics
    if [[ ${#response_times[@]} -gt 0 ]]; then
        local total_sum=0
        local overall_min=${response_times[0]}
        local overall_max=${response_times[0]}
        
        for time in "${response_times[@]}"; do
            if [[ "$time" =~ ^[0-9.]+$ ]]; then
                total_sum=$(echo "$total_sum + $time" | bc -l 2>/dev/null || echo "$total_sum")
                if [[ -n "$total_sum" ]] && (( $(echo "$time < $overall_min" | bc -l 2>/dev/null || echo "0") )); then
                    overall_min=$time
                fi
                if [[ -n "$total_sum" ]] && (( $(echo "$time > $overall_max" | bc -l 2>/dev/null || echo "0") )); then
                    overall_max=$time
                fi
            fi
        done
        
        local overall_avg=""
        local success_rate=""
        local total_requests=$((${#endpoints[@]} * PERFORMANCE_TESTS))
        
        if [[ "$total_sum" =~ ^[0-9.]+$ && ${#response_times[@]} -gt 0 ]]; then
            overall_avg=$(echo "scale=3; $total_sum / ${#response_times[@]}" | bc -l 2>/dev/null || echo "N/A")
        else
            overall_avg="N/A"
        fi
        
        if [[ $total_requests -gt 0 ]]; then
            success_rate=$(echo "scale=1; (($total_requests - $failed_requests) * 100) / $total_requests" | bc -l 2>/dev/null || echo "N/A")
        else
            success_rate="N/A"
        fi
        
        log_perf "Overall Performance Summary (curl-optimized):"
        log_perf "  Total Requests: $total_requests"
        log_perf "  Successful: $(($total_requests - $failed_requests))"
        log_perf "  Failed: $failed_requests"
        log_perf "  Success Rate: ${success_rate}%"
        log_perf "  Average Response Time: ${overall_avg}s"
        log_perf "  Min Response Time: ${overall_min}s"
        log_perf "  Max Response Time: ${overall_max}s"
        log_perf "  API Server CPU: ${api_server_cpu}m"
        log_perf "  API Server Memory: ${api_server_memory}Mi"
        log_perf "  ETCD CPU: ${etcd_cpu}m"
        log_perf "  ETCD Memory: ${etcd_memory}Mi"
        log_perf "  Total Pods: $total_pods"
        log_perf "  Total Services: $total_services"
        log_perf "  Metrics Collection Time: ${metrics_collection_time}s"
        
        # Create CSV output files (same as original)
        local summary_csv="/tmp/kubemark_performance_summary_curl.csv"
        local detailed_csv="/tmp/kubemark_performance_detailed_curl.csv"
        
        # Write headers if files don't exist
        if [[ ! -f "$summary_csv" ]]; then
            cat > "$summary_csv" <<EOF
Timestamp,Batch,TotalNodes,BatchSize,OverallAvgResponseTime,MinResponseTime,MaxResponseTime,SuccessRate,TotalRequests,FailedRequests,ApiServerCPU,ApiServerMemory,EtcdCPU,EtcdMemory,TotalPods,TotalServices,TotalDeployments,TotalConfigMaps,TotalSecrets,MetricsCollectionTime
EOF
        fi
        
        if [[ ! -f "$detailed_csv" ]]; then
            cat > "$detailed_csv" <<EOF
Timestamp,Batch,TotalNodes,Endpoint,AvgResponseTime,MinResponseTime,MaxResponseTime,FailedRequests
EOF
        fi
        
        # Calculate batch size for this batch
        local batch_size_for_csv=""
        if [[ -n "${batches_plan:-}" ]]; then
            for batch_info in "${batches_plan[@]}"; do
                IFS=':' read -r b_num b_size <<< "$batch_info"
                if [[ $b_num -eq $batch_number ]]; then
                    batch_size_for_csv=$b_size
                    break
                fi
            done
        fi
        [[ -z "$batch_size_for_csv" ]] && batch_size_for_csv="N/A"
        
        # Write summary data to CSV
        local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
        echo "$timestamp,$batch_number,$total_nodes,$batch_size_for_csv,$overall_avg,$overall_min,$overall_max,$success_rate,$total_requests,$failed_requests,$api_server_cpu,$api_server_memory,$etcd_cpu,$etcd_memory,$total_pods,$total_services,$total_deployments,$total_configmaps,$total_secrets,$metrics_collection_time" >> "$summary_csv"
        
        # Write detailed endpoint data to CSV
        for i in "${!endpoint_names[@]}"; do
            local endpoint_name="${endpoint_names[$i]}"
            local endpoint_key=$(echo "$endpoint_name" | tr ' ' '_' | tr '-' '_')
            if [[ -n "${endpoint_stats[$endpoint_key]:-}" ]]; then
                IFS=',' read -r avg min max failures <<< "${endpoint_stats[$endpoint_key]}"
                echo "$timestamp,$batch_number,$total_nodes,$endpoint_name,$avg,$min,$max,$failures" >> "$detailed_csv"
            fi
        done
        
        # Store performance data for final report
        echo "BATCH_${batch_number}_NODES_${total_nodes}_AVG_${overall_avg}_MIN_${overall_min}_MAX_${overall_max}_SUCCESS_${success_rate}" >> /tmp/kubemark_performance_curl.log
        
        log_perf "📊 Curl-optimized performance data written to:"
        log_perf "  Summary CSV: $summary_csv"
        log_perf "  Detailed CSV: $detailed_csv"
    else
        log_perf "⚠️  All API requests failed!"
        
        # Still write to CSV with failed data (same as original)
        local summary_csv="/tmp/kubemark_performance_summary_curl.csv"
        local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
        
        if [[ ! -f "$summary_csv" ]]; then
            cat > "$summary_csv" <<EOF
Timestamp,Batch,TotalNodes,BatchSize,OverallAvgResponseTime,MinResponseTime,MaxResponseTime,SuccessRate,TotalRequests,FailedRequests,ApiServerCPU,ApiServerMemory,EtcdCPU,EtcdMemory,TotalPods,TotalServices,TotalDeployments,TotalConfigMaps,TotalSecrets,MetricsCollectionTime
EOF
        fi
        
        local batch_size_for_csv=""
        if [[ -n "${batches_plan:-}" ]]; then
            for batch_info in "${batches_plan[@]}"; do
                IFS=':' read -r b_num b_size <<< "$batch_info"
                if [[ $b_num -eq $batch_number ]]; then
                    batch_size_for_csv=$b_size
                    break
                fi
            done
        fi
        [[ -z "$batch_size_for_csv" ]] && batch_size_for_csv="N/A"
        
        local total_requests=$((${#endpoints[@]} * PERFORMANCE_TESTS))
        echo "$timestamp,$batch_number,$total_nodes,$batch_size_for_csv,FAILED,FAILED,FAILED,0.0,$total_requests,$total_requests,$api_server_cpu,$api_server_memory,$etcd_cpu,$etcd_memory,$total_pods,$total_services,$total_deployments,$total_configmaps,$total_secrets,$metrics_collection_time" >> "$summary_csv"
        
        echo "BATCH_${batch_number}_NODES_${total_nodes}_ALL_FAILED" >> /tmp/kubemark_performance_curl.log
    fi
    
    echo ""
}

# Function to create kubeconfig for hollow nodes (using internal Kubernetes service IP)
create_hollow_node_kubeconfig() {
    local kubeconfig_path="/tmp/hollow-node-kubeconfig"
    local kubernetes_service_ip=$(kubectl get svc kubernetes -n default -o jsonpath='{.spec.clusterIP}')
    local api_server="https://${kubernetes_service_ip}:443"  # Using internal Kubernetes service IP
    local cluster_name=$(kubectl config view --minify -o jsonpath='{.clusters[0].name}')
    local ca_data=$(kubectl config view --minify --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
    
    log_info "Creating kubeconfig for hollow nodes to connect to internal API service: $api_server"
    
    kubectl create serviceaccount hollow-node-sa -n "$TEST_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
    
    cat <<EOF | kubectl apply -f -
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: hollow-node-incremental-role
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: [""]
  resources: ["nodes/status"]
  verbs: ["patch", "update"]
- apiGroups: [""]
  resources: ["nodes/proxy"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods/status"]
  verbs: ["patch", "update"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["endpoints"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["certificates.k8s.io"]
  resources: ["certificatesigningrequests"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: ["node.k8s.io"]
  resources: ["runtimeclasses"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["storage.k8s.io"]
  resources: ["csinodes"]
  verbs: ["create", "delete", "get", "list", "patch", "update", "watch"]
- apiGroups: ["storage.k8s.io"]
  resources: ["csidrivers"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["storage.k8s.io"]
  resources: ["volumeattachments"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: hollow-node-incremental-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: hollow-node-incremental-role
subjects:
- kind: ServiceAccount
  name: hollow-node-sa
  namespace: $TEST_NAMESPACE
EOF

    log_info "Waiting for service account token..."
    sleep 5
    
    local token_name=$(kubectl get serviceaccount hollow-node-sa -n "$TEST_NAMESPACE" -o jsonpath='{.secrets[0].name}' 2>/dev/null || echo "")
    local token=""
    
    if [[ -n "$token_name" ]]; then
        token=$(kubectl get secret "$token_name" -n "$TEST_NAMESPACE" -o jsonpath='{.data.token}' | base64 -d)
    else
        token=$(kubectl create token hollow-node-sa -n "$TEST_NAMESPACE" --duration=24h)
    fi
    
    if [[ -z "$token" ]]; then
        log_error "Failed to get service account token"
        return 1
    fi
    
    cat > "$kubeconfig_path" <<EOF
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: $ca_data
    server: $api_server
  name: $cluster_name
contexts:
- context:
    cluster: $cluster_name
    user: hollow-node-user
  name: hollow-node-context
current-context: hollow-node-context
users:
- name: hollow-node-user
  user:
    token: $token
EOF
    
    kubectl create secret generic hollow-node-kubeconfig \
        --from-file=kubeconfig="$kubeconfig_path" \
        -n "$TEST_NAMESPACE" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    rm -f "$kubeconfig_path"
    
    log_info "Kubeconfig secret created for hollow nodes"
}

# Function to calculate next batch size exponentially (unchanged from original)
calculate_batch_size() {
    local batch_number="$1"
    if [[ ! "$batch_number" =~ ^[0-9]+$ ]]; then
        echo "1"
        return
    fi
    local batch_size=$(echo "scale=0; $INITIAL_BATCH_SIZE * ($EXPONENTIAL_FACTOR ^ ($batch_number - 1))" | bc -l 2>/dev/null)
    if [[ ! "$batch_size" =~ ^[0-9.]+$ ]]; then
        echo "1"
        return
    fi
    batch_size=${batch_size%.*}
    echo "$batch_size"
}

# Function to calculate total nodes planned and actual batches needed (unchanged from original)
calculate_batches_plan() {
    local current_total=0
    local batch_num=1
    local batches_info=()
    
    while [[ $current_total -lt $MAX_NODES && $batch_num -le $MAX_BATCHES ]]; do
        local batch_size=$(calculate_batch_size "$batch_num")
        local nodes_in_batch=$batch_size
        
        if [[ $(( current_total + batch_size )) -gt $MAX_NODES ]]; then
            nodes_in_batch=$(( MAX_NODES - current_total ))
        fi
        
        batches_info+=("$batch_num:$nodes_in_batch")
        current_total=$(( current_total + nodes_in_batch ))
        
        if [[ $current_total -ge $MAX_NODES ]]; then
            break
        fi
        
        batch_num=$(( batch_num + 1 ))
    done
    
    echo "${batches_info[@]}"
}

# Function to create a batch of hollow nodes (unchanged from original)
create_hollow_node_batch() {
    local batch_number="$1"
    local batch_size="$2"
    local start_idx="$3"
    local end_idx="$4"
    
    log_batch "Creating batch $batch_number: hollow nodes $start_idx to $end_idx (batch size: $batch_size)"
    
    for (( j=start_idx; j<=end_idx; j++ )); do
        cat <<EOF | kubectl apply -f - &
apiVersion: v1
kind: Pod
metadata:
  name: hollow-node-$j
  namespace: $TEST_NAMESPACE
  labels:
    app: hollow-node
    batch: "batch-$batch_number"
    node-id: "hollow-node-$j"
spec:
  imagePullSecrets:
  - name: acrvapa17-secret
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: node-role.kubernetes.io/control-plane
            operator: DoesNotExist
          - key: kubemark
            operator: DoesNotExist
          - key: node-role.kubernetes.io/worker
            operator: Exists
          - key: node.kubernetes.io/instance-type
            operator: In
            values:
            - k3s
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              app: hollow-node
          topologyKey: kubernetes.io/hostname
  volumes:
  - name: kubeconfig-volume
    secret:
      secretName: hollow-node-kubeconfig
  - name: logs-volume
    emptyDir: {}
  containers:
  - name: hollow-kubelet
    image: $KUBEMARK_IMAGE
    env:
    - name: NODE_NAME
      value: "hollow-node-$j"
    command: [
      "/go-runner",
      "-log-file=/var/log/kubelet-hollow-node-$j.log",
      "-also-stdout=true",
      "/kubemark",
      "--morph=kubelet",
      "--name=hollow-node-$j",
      "--kubeconfig=/kubeconfig/kubeconfig",
      "--node-labels=kubemark=true,incremental-test=true,batch=batch-$batch_number",
      "--max-pods=110",
      "--use-host-image-service=false",
      "--node-lease-duration-seconds=40",
      "--node-status-update-frequency=10s",
      "--node-status-report-frequency=5m",
      "--v=4"
    ]
    volumeMounts:
    - name: kubeconfig-volume
      mountPath: /kubeconfig
      readOnly: true
    - name: logs-volume
      mountPath: /var/log
    resources:
      requests:
        cpu: "20m"
        memory: "50Mi"
      limits:
        cpu: "100m"
        memory: "200Mi"
  - name: hollow-proxy
    image: $KUBEMARK_IMAGE
    env:
    - name: NODE_NAME
      value: "hollow-node-$j"
    command: [
      "/go-runner",
      "-log-file=/var/log/kubeproxy-hollow-node-$j.log",
      "-also-stdout=true",
      "/kubemark",
      "--morph=proxy",
      "--name=hollow-node-$j",
      "--kubeconfig=/kubeconfig/kubeconfig",
      "--v=4"
    ]
    volumeMounts:
    - name: kubeconfig-volume
      mountPath: /kubeconfig
      readOnly: true
    - name: logs-volume
      mountPath: /var/log
    resources:
      requests:
        cpu: "10m"
        memory: "25Mi"
      limits:
        cpu: "50m"
        memory: "100Mi"
  restartPolicy: Always
EOF
    done
    
    wait
    
    log_batch "Created batch $batch_number: hollow-node-$start_idx to hollow-node-$end_idx (batch size: $batch_size)"
}

# Function to wait for hollow nodes to become ready - optimized with curl
wait_for_hollow_nodes() {
    local expected_total="$1"
    local timeout="$2"
    local start_time=$(date +%s)
    
    log_info "Waiting up to ${timeout}s for $expected_total hollow nodes to become ready..."
    
    while [[ $(($(date +%s) - start_time)) -lt $timeout ]]; do
        local counts=$(get_kubemark_node_counts true)
        IFS=':' read -r registered_nodes ready_nodes <<< "$counts"
        
        log_info "Progress: $ready_nodes/$expected_total nodes ready ($registered_nodes registered)"
        
        if [[ $ready_nodes -ge $expected_total ]]; then
            log_success "✅ All $expected_total hollow nodes are ready!"
            return 0
        fi
        
        sleep 10
    done
    
    local final_counts=$(get_kubemark_node_counts true)
    IFS=':' read -r _ final_ready <<< "$final_counts"
    log_warn "⚠️  Timeout waiting for hollow nodes. Only $final_ready/$expected_total nodes are ready"
    return 1
}

# Function to show current status - optimized with curl
show_status() {
    local batch_number="$1"
    local batch_size="$2"
    local total_expected="$3"
    
    echo ""
    log_info "=== STATUS AFTER BATCH $batch_number (size: $batch_size) ==="
    
    # Show pod status (still use kubectl for namespace-scoped resources)
    local total_pods=$(kubectl get pods -n "$TEST_NAMESPACE" --no-headers 2>/dev/null | wc -l)
    local running_pods=$(kubectl get pods -n "$TEST_NAMESPACE" --no-headers 2>/dev/null | grep -c " Running " || echo "0")
    local failed_pods=$(kubectl get pods -n "$TEST_NAMESPACE" --no-headers 2>/dev/null | grep -c " Error\|Failed\|CrashLoopBackOff " || echo "0")
    
    log_info "Pods: $total_pods total, $running_pods running, $failed_pods failed"
    
    # Show node status using optimized API calls
    local counts=$(get_kubemark_node_counts)
    IFS=':' read -r registered_nodes ready_nodes <<< "$counts"
    
    log_info "Nodes: $ready_nodes/$total_expected ready ($registered_nodes registered)"
    
    # Show detailed pod status for this batch
    log_info "Batch $batch_number pod details:"
    kubectl get pods -n "$TEST_NAMESPACE" -l batch="batch-$batch_number" --no-headers 2>/dev/null | while read line; do
        echo "  $line"
    done
    
    echo ""
}

# Function to cleanup test resources - optimized with curl
cleanup_test() {
    log_info "Cleaning up incremental test resources..."
    
    # Force delete all pods (still use kubectl for namespace operations)
    log_info "Force deleting pods..."
    kubectl get pods -n "$TEST_NAMESPACE" --no-headers 2>/dev/null | awk '{print $1}' | xargs -r kubectl delete pod -n "$TEST_NAMESPACE" --force --grace-period=0 --ignore-not-found=true
    
    # Delete namespace
    log_info "Deleting namespace..."
    kubectl delete namespace "$TEST_NAMESPACE" --ignore-not-found=true --timeout=60s
    
    # Clean up RBAC
    kubectl delete clusterrolebinding hollow-node-incremental-binding --ignore-not-found=true
    kubectl delete clusterrole hollow-node-incremental-role --ignore-not-found=true
    
    # Remove hollow nodes using optimized API call
    log_info "Removing hollow nodes from cluster..."
    
    local node_names=$(get_kubemark_node_names)
    if [[ -n "$node_names" ]]; then
        echo "$node_names" | xargs -r kubectl delete node --ignore-not-found=true
    else
        log_warn "API call failed, using kubectl fallback for node cleanup"
        kubectl get nodes -l kubemark=true --no-headers 2>/dev/null | awk '{print $1}' | xargs -r kubectl delete node --ignore-not-found=true
    fi
    
    # Cleanup temporary certificate files
    rm -f /tmp/client.crt /tmp/client.key /tmp/ca.crt
    
    log_info "Cleanup completed"
}

# Function to setup initial environment (unchanged from original)
setup_environment() {
    log_info "Setting up environment for incremental hollow node test..."
    
    kubectl create namespace "$TEST_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
    
    if kubectl get secret sternvapa17-secret -n default >/dev/null 2>&1; then
        log_info "Copying acrvapa17-secret for image pull authentication..."
        kubectl get secret acrvapa17-secret -n default -o yaml | \
        sed "s/namespace: default/namespace: $TEST_NAMESPACE/" | \
        kubectl apply -f -
    elif kubectl get secret acr-test-secret -n default >/dev/null 2>&1; then
        log_info "Copying ACR secret for image pull authentication..."
        kubectl get secret acr-test-secret -n default -o yaml | \
        sed "s/namespace: default/namespace: $TEST_NAMESPACE/" | \
        sed "s/name: acr-test-secret/name: acrvapa17-secret/" | \
        kubectl apply -f -
    elif kubectl get secret acrvapa10-secret -n default >/dev/null 2>&1; then
        log_info "Copying acrvapa10-secret for image pull authentication..."
        kubectl get secret acrvapa10-secret -n default -o yaml | \
        sed "s/namespace: default/namespace: $TEST_NAMESPACE/" | \
        sed "s/name: acrvapa10-secret/name: acr-test-secret/" | \
        kubectl apply -f -
    elif kubectl get secret acr-secret -n default >/dev/null 2>&1; then
        log_info "Copying ACR secret for image pull authentication..."
        kubectl get secret acr-secret -n default -o yaml | \
        sed "s/namespace: default/namespace: $TEST_NAMESPACE/" | \
        sed "s/name: acr-secret/name: acr-test-secret/" | \
        kubectl apply -f -
    else
        log_warn "No ACR secret found in default namespace. Image pull may fail for private registries."
    fi
    
    create_hollow_node_kubeconfig
}

# Main function (mostly unchanged, just updated file references)
main() {
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --initial-batch-size)
                INITIAL_BATCH_SIZE="$2"
                shift 2
                ;;
            --exponential-factor)
                EXPONENTIAL_FACTOR="$2"
                shift 2
                ;;
            --max-nodes)
                MAX_NODES="$2"
                shift 2
                ;;
            --max-batches)
                MAX_BATCHES="$2"
                shift 2
                ;;
            --timeout)
                WAIT_TIMEOUT="$2"
                shift 2
                ;;
            --perf-wait)
                PERFORMANCE_WAIT="$2"
                shift 2
                ;;
            --perf-tests)
                PERFORMANCE_TESTS="$2"
                shift 2
                ;;
            --cleanup-only)
                cleanup_test
                exit 0
                ;;
            -h|--help)
                cat <<EOF
Usage: $0 [OPTIONS]

This script adds hollow nodes incrementally in exponentially increasing batches,
waiting for each batch to become ready before adding the next batch.
This version is optimized with curl for better performance.

OPTIONS:
  --initial-batch-size NUM    Starting number of nodes per batch (default: $INITIAL_BATCH_SIZE)
  --exponential-factor NUM    Factor to multiply batch size each iteration (default: $EXPONENTIAL_FACTOR)
  --max-nodes NUM            Maximum total nodes to create (default: $MAX_NODES)
  --max-batches NUM          Maximum number of batches as safety limit (default: $MAX_BATCHES)
  --timeout SECONDS          Timeout to wait for each batch (default: $WAIT_TIMEOUT)
  --perf-wait SECONDS        Wait time before measuring performance (default: $PERFORMANCE_WAIT)
  --perf-tests NUM           Number of API calls per endpoint for performance test (default: $PERFORMANCE_TESTS)
  --cleanup-only             Only cleanup test resources
  -h, --help                 Show this help

ENVIRONMENT VARIABLES:
  KUBEMARK_IMAGE        Kubemark docker image to use (default: $KUBEMARK_IMAGE)

This curl-optimized version uses direct API calls for node operations instead of kubectl,
resulting in significantly better performance for large clusters.
EOF
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
    
    echo "🚀 Starting Exponential Incremental Hollow Node Addition Test (curl-optimized)"
    echo "Configuration:"
    echo "  Initial Batch Size: $INITIAL_BATCH_SIZE nodes"
    echo "  Exponential Factor: $EXPONENTIAL_FACTOR"
    echo "  Max Nodes: $MAX_NODES nodes"
    echo "  Max Batches: $MAX_BATCHES batches"
    echo "  Wait Timeout: ${WAIT_TIMEOUT}s per batch"
    echo "  Performance Wait: ${PERFORMANCE_WAIT}s before measuring"
    echo "  Performance Tests: ${PERFORMANCE_TESTS} calls per endpoint"
    echo "  Kubemark Image: $KUBEMARK_IMAGE"
    echo "  Optimization: curl + compression + table format"
    echo ""
    
    # Calculate batches plan
    local batches_plan=($(calculate_batches_plan))
    local total_batches=${#batches_plan[@]}
    
    echo "📋 Batch Plan:"
    local running_total=0
    for batch_info in "${batches_plan[@]}"; do
        IFS=':' read -r batch_num batch_size <<< "$batch_info"
        running_total=$(( running_total + batch_size ))
        echo "  Batch $batch_num: $batch_size nodes (total: $running_total)"
    done
    echo ""
    
    # Initialize performance log and CSV files
    rm -f /tmp/kubemark_performance_curl.log
    rm -f /tmp/kubemark_performance_summary_curl.csv
    rm -f /tmp/kubemark_performance_detailed_curl.csv
    
    # Setup environment
    setup_environment
    
    # Test API access before proceeding
    if ! test_api_access; then
        log_error "API access test failed, aborting"
        exit 1
    fi
    
    # Check for existing hollow nodes and adjust starting point
    log_info "Checking for existing hollow nodes..."
    local existing_counts=$(get_kubemark_node_counts true)
    IFS=':' read -r existing_registered existing_ready <<< "$existing_counts"
    
    if [[ $existing_registered -gt 0 ]]; then
        log_info "Found $existing_registered existing kubemark nodes ($existing_ready ready)"
        
        if [[ $existing_registered -ge $MAX_NODES ]]; then
            log_warn "Already have $existing_registered nodes, which meets or exceeds target of $MAX_NODES"
            log_info "Current node status: $existing_ready/$existing_registered ready"
            log_info "Use --cleanup-only to remove existing nodes, or increase --max-nodes"
            exit 0
        fi
        
        # Find the highest existing node number to avoid conflicts
        log_info "Detecting highest existing node number to avoid conflicts..."
        local node_names_result=$(get_kubemark_node_names)
        local highest_node_num=0
        
        while IFS= read -r node_name; do
            if [[ "$node_name" =~ hollow-node-([0-9]+)$ ]]; then
                local node_num="${BASH_REMATCH[1]}"
                if [[ $node_num -gt $highest_node_num ]]; then
                    highest_node_num=$node_num
                fi
            fi
        done <<< "$node_names_result"
        
        log_info "Highest existing node number: $highest_node_num"
        log_info "Will start new nodes from hollow-node-$((highest_node_num + 1))"
        
        # Adjust the maximum to account for existing nodes
        local remaining_nodes=$((MAX_NODES - existing_registered))
        if [[ $remaining_nodes -le 0 ]]; then
            log_info "No additional nodes needed (have $existing_registered, target $MAX_NODES)"
            exit 0
        fi
        
        log_info "Will create $remaining_nodes additional nodes (from $((highest_node_num + 1)) to $((highest_node_num + remaining_nodes)))"
        
        # Update MAX_NODES to reflect only the additional nodes needed
        MAX_NODES=$remaining_nodes
        
        # Recalculate batches plan with adjusted numbers
        batches_plan=($(calculate_batches_plan))
        total_batches=${#batches_plan[@]}
        
        echo ""
        echo "📋 Adjusted Batch Plan (adding to existing $existing_registered nodes):"
        local running_total=$existing_registered
        for batch_info in "${batches_plan[@]}"; do
            IFS=':' read -r batch_num batch_size <<< "$batch_info"
            running_total=$(( running_total + batch_size ))
            echo "  Batch $batch_num: $batch_size nodes (total: $running_total)"
        done
        echo ""
    else
        log_info "No existing kubemark nodes found, starting fresh"
        highest_node_num=0
    fi
    
    # Add batches incrementally
    local current_total_nodes=$existing_registered
    local node_counter=$((highest_node_num + 1))
    
    for batch_info in "${batches_plan[@]}"; do
        IFS=':' read -r batch_number actual_batch_size <<< "$batch_info"
        local start_idx=$node_counter
        local end_idx=$(( start_idx + actual_batch_size - 1 ))
        current_total_nodes=$(( current_total_nodes + actual_batch_size ))
        
        echo "📦 Processing Batch $batch_number/$total_batches (Exponential size: $actual_batch_size)"
        
        create_hollow_node_batch "$batch_number" "$actual_batch_size" "$start_idx" "$end_idx"
        node_counter=$(( end_idx + 1 ))
        
        if wait_for_hollow_nodes "$current_total_nodes" "$WAIT_TIMEOUT"; then
            log_success "✅ Batch $batch_number completed successfully!"
        else
            log_warn "⚠️  Batch $batch_number did not fully complete, but continuing..."
        fi
        
        show_status "$batch_number" "$actual_batch_size" "$current_total_nodes"
        
        if [[ $PERFORMANCE_WAIT -gt 0 ]]; then
            log_info "Waiting ${PERFORMANCE_WAIT}s for system to stabilize before measuring performance..."
            sleep "$PERFORMANCE_WAIT"
        fi
        
        measure_cluster_performance "$batch_number" "$current_total_nodes"
        
        if [[ $batch_number -lt $total_batches ]]; then
            log_info "Waiting 10 seconds before next batch..."
            sleep 10
        fi
        
        if [[ $current_total_nodes -ge $MAX_NODES ]]; then
            log_info "✋ Reached maximum nodes limit ($MAX_NODES), stopping..."
            break
        fi
    done
    
    # Final summary
    echo ""
    log_success "🎯 Exponential incremental hollow node addition completed! (curl-optimized)"
    echo ""
    log_info "Final Summary:"
    local final_counts=$(get_kubemark_node_counts true)
    IFS=':' read -r _ final_ready <<< "$final_counts"
    log_info "  Target: $current_total_nodes hollow nodes"
    log_info "  Achieved: $final_ready ready nodes"
    log_info "  Total Batches: $total_batches"
    
    if [[ $final_ready -eq $current_total_nodes ]]; then
        log_success "  ✅ All nodes successfully added!"
    else
        log_warn "  ⚠️  Only $final_ready/$current_total_nodes nodes became ready"
    fi
    
    # Display performance summary
    if [[ -f /tmp/kubemark_performance_curl.log ]]; then
        echo ""
        log_perf "📊 Performance Summary Across All Batches (curl-optimized):"
        echo ""
        printf "%-8s %-10s %-8s %-10s %-10s %-10s %-10s\n" "Batch" "BatchSize" "Nodes" "Avg(s)" "Min(s)" "Max(s)" "Success%"
        echo "--------------------------------------------------------------------------------"
        
        while IFS= read -r line; do
            if [[ $line =~ BATCH_([0-9]+)_NODES_([0-9]+)_AVG_([0-9.]+)_MIN_([0-9.]+)_MAX_([0-9.]+)_SUCCESS_([0-9.]+) ]]; then
                local batch_num="${BASH_REMATCH[1]}"
                local total_nodes="${BASH_REMATCH[2]}"
                local actual_batch_size=""
                
                for batch_info in "${batches_plan[@]}"; do
                    IFS=':' read -r b_num b_size <<< "$batch_info"
                    if [[ $b_num -eq $batch_num ]]; then
                        actual_batch_size=$b_size
                        break
                    fi
                done
                
                printf "%-8s %-10s %-8s %-10s %-10s %-10s %-10s\n" \
                    "$batch_num" \
                    "${actual_batch_size:-N/A}" \
                    "$total_nodes" \
                    "${BASH_REMATCH[3]}" \
                    "${BASH_REMATCH[4]}" \
                    "${BASH_REMATCH[5]}" \
                    "${BASH_REMATCH[6]}"
            elif [[ $line =~ BATCH_([0-9]+)_NODES_([0-9]+)_ALL_FAILED ]]; then
                local batch_num="${BASH_REMATCH[1]}"
                local total_nodes="${BASH_REMATCH[2]}"
                local this_batch_size=$(calculate_batch_size "$batch_num")
                
                printf "%-8s %-10s %-8s %-10s %-10s %-10s %-10s\n" \
                    "$batch_num" \
                    "$this_batch_size" \
                    "$total_nodes" \
                    "FAILED" "FAILED" "FAILED" "0.0"
            fi
        done < /tmp/kubemark_performance_curl.log
        
        echo ""
        log_perf "Performance data saved to: /tmp/kubemark_performance_curl.log"
    fi
    
    # Display CSV file information
    echo ""
    log_perf "📈 CSV Performance Data Files (curl-optimized):"
    if [[ -f /tmp/kubemark_performance_summary_curl.csv ]]; then
        local summary_rows=$(wc -l < /tmp/kubemark_performance_summary_curl.csv)
        log_perf "  Summary CSV: /tmp/kubemark_performance_summary_curl.csv ($((summary_rows - 1)) data rows)"
    fi
    
    if [[ -f /tmp/kubemark_performance_detailed_curl.csv ]]; then
        local detailed_rows=$(wc -l < /tmp/kubemark_performance_detailed_curl.csv)
        log_perf "  Detailed CSV: /tmp/kubemark_performance_detailed_curl.csv ($((detailed_rows - 1)) data rows)"
    fi
    
    echo ""
    log_perf "💡 Curl Optimization Benefits:"
    log_perf "  • Direct API calls without kubectl overhead"
    log_perf "  • Compressed HTTP requests for reduced network usage"
    log_perf "  • Table format for lighter node queries"
    log_perf "  • Pagination for memory-efficient large cluster handling"
    log_perf "  • Single API calls for multiple data points"
    
    echo ""
    log_info "To see the nodes: kubectl get nodes -l kubemark=true"
    log_info "To see the pods: kubectl get pods -n $TEST_NAMESPACE"
    log_info "To cleanup: $0 --cleanup-only"
    log_info "To view curl-optimized CSV data: cat /tmp/kubemark_performance_summary_curl.csv"
    log_info "To analyze detailed data: cat /tmp/kubemark_performance_detailed_curl.csv"
    echo ""
}

# Check prerequisites
if ! command -v kubectl &> /dev/null; then
    log_error "kubectl is required but not found"
    exit 1
fi

if ! command -v bc &> /dev/null; then
    log_error "bc (calculator) is required but not found. Please install bc package."
    exit 1
fi

if ! command -v jq &> /dev/null; then
    log_error "jq is required for JSON processing but not found. Please install jq package."
    exit 1
fi

if ! kubectl cluster-info &> /dev/null; then
    log_error "Cannot access Kubernetes cluster"
    exit 1
fi

# Run main function with all arguments
main "$@"
