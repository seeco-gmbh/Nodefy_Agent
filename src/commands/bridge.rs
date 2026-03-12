use serde_json::Value;
use std::time::Duration;
use tauri::{AppHandle, State};
use tracing::info;

use crate::events::BridgeStatus;
use crate::state::AgentState;

#[tauri::command]
pub async fn bridge_connect(
    app: AppHandle,
    state: State<'_, AgentState>,
    url: String,
    api_key: Option<String>,
) -> Result<(), String> {
    info!("Bridge connect requested: {}", url);

    // Save to config
    {
        let mut config = state.config.lock().await;
        config.bridge_url = url.clone();
        if let Some(ref key) = api_key {
            config.bridge_api_key = key.clone();
        }
        let _ = crate::config::save_config(&config);
    }

    state
        .bridge
        .connect(app, url, api_key)
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_disconnect(state: State<'_, AgentState>) -> Result<(), String> {
    info!("Bridge disconnect requested");
    state.bridge.disconnect().await.map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_status(state: State<'_, AgentState>) -> Result<BridgeStatus, String> {
    let s = state.bridge.get_status().await;
    Ok(BridgeStatus {
        connected: s.connected,
        authenticated: s.authenticated,
        url: s.url,
    })
}

#[tauri::command]
pub async fn bridge_send(
    state: State<'_, AgentState>,
    action: String,
    payload: Value,
    expected_method: Option<String>,
    timeout_ms: Option<u64>,
) -> Result<Value, String> {
    let timeout = Duration::from_millis(timeout_ms.unwrap_or(10_000));

    let expected = expected_method.unwrap_or_default();
    if expected.is_empty() {
        state.bridge.send(&action, payload).await.map_err(|e| e.to_string())?;
        Ok(Value::Null)
    } else {
        state
            .bridge
            .send_and_wait(&action, payload, &expected, timeout)
            .await
            .map_err(|e| e.to_string())
    }
}

// Convenience wrappers matching the Go bridge handlers

#[tauri::command]
pub async fn bridge_create_component(
    state: State<'_, AgentState>,
    component_type: String,
    payload: Option<Value>,
) -> Result<Value, String> {
    let mut data = payload.unwrap_or_else(|| Value::Object(Default::default()));
    if let Some(obj) = data.as_object_mut() {
        obj.insert("componentType".to_string(), Value::String(component_type.clone()));
    }
    state
        .bridge
        .send_and_wait("CreateComponent", data, "Created", Duration::from_secs(10))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_update_component(
    state: State<'_, AgentState>,
    component_id: String,
    payload: Value,
) -> Result<Value, String> {
    let mut data = payload;
    if let Some(obj) = data.as_object_mut() {
        obj.insert("componentId".to_string(), Value::String(component_id));
    }
    state
        .bridge
        .send_and_wait("UpdateComponent", data, "Updated", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_delete_component(
    state: State<'_, AgentState>,
    component_id: String,
) -> Result<Value, String> {
    let payload = serde_json::json!({ "componentId": component_id });
    state
        .bridge
        .send_and_wait("DeleteComponent", payload, "Deleted", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_get_component_info(
    state: State<'_, AgentState>,
    component_id: String,
    mode: Option<String>,
    port_id: Option<String>,
) -> Result<Value, String> {
    let mode = mode.unwrap_or_else(|| "details".to_string());
    let expected = match mode.as_str() {
        "ports" => "ComponentPorts",
        "port-details" => "PortDetails",
        _ => "ComponentDetails",
    };

    let mut payload = serde_json::json!({ "componentId": component_id, "mode": mode });
    if let Some(pid) = port_id {
        payload["portId"] = Value::String(pid);
    }

    state
        .bridge
        .send_and_wait("GetComponentInfo", payload, expected, Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_execute(
    state: State<'_, AgentState>,
    component_id: String,
    component_type: Option<String>,
    payload: Option<Value>,
) -> Result<Value, String> {
    let component_type = component_type.unwrap_or_else(|| "Network".to_string());
    let key = format!("{}Id", component_type.to_lowercase());

    let mut data = payload.unwrap_or_else(|| Value::Object(Default::default()));
    if let Some(obj) = data.as_object_mut() {
        obj.insert(key, Value::String(component_id));
    }

    state
        .bridge
        .send_and_wait("Execute", data, "ExecutionCompleted", Duration::from_secs(30))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_warmup(
    state: State<'_, AgentState>,
    network_id: String,
) -> Result<Value, String> {
    let payload = serde_json::json!({ "networkId": network_id });
    state
        .bridge
        .send_and_wait("WarmUp", payload, "ExecutionCompleted", Duration::from_secs(30))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_connect_ports(
    state: State<'_, AgentState>,
    payload: Value,
) -> Result<Value, String> {
    state
        .bridge
        .send_and_wait("ConnectPorts", payload, "Created", Duration::from_secs(10))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_add_port(
    state: State<'_, AgentState>,
    component_id: String,
    payload: Value,
) -> Result<Value, String> {
    let mut data = payload;
    if let Some(obj) = data.as_object_mut() {
        obj.insert("componentId".to_string(), Value::String(component_id));
    }
    state
        .bridge
        .send_and_wait("AddPortToComponent", data, "Created", Duration::from_secs(10))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_delete_port(
    state: State<'_, AgentState>,
    component_id: String,
    port_id: String,
) -> Result<Value, String> {
    let payload = serde_json::json!({ "componentId": component_id, "portId": port_id });
    state
        .bridge
        .send_and_wait("DeletePortFromComponent", payload, "Deleted", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_update_port(
    state: State<'_, AgentState>,
    port_id: String,
    payload: Value,
) -> Result<Value, String> {
    let mut data = payload;
    if let Some(obj) = data.as_object_mut() {
        obj.insert("portId".to_string(), Value::String(port_id));
    }
    state
        .bridge
        .send_and_wait("UpdatePort", data, "PortUpdated", Duration::from_secs(2))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_get_connection_info(
    state: State<'_, AgentState>,
    container_id: String,
    mode: Option<String>,
) -> Result<Value, String> {
    let mode = mode.unwrap_or_else(|| "connections".to_string());
    let expected = if mode == "available-ports" { "AvailablePorts" } else { "Connections" };
    let payload = serde_json::json!({ "containerId": container_id, "mode": mode });
    state
        .bridge
        .send_and_wait("GetConnectionInfo", payload, expected, Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_export_component(
    state: State<'_, AgentState>,
    component_id: String,
    component_type: Option<String>,
) -> Result<Value, String> {
    let payload = serde_json::json!({
        "id": component_id,
        "componentType": component_type.unwrap_or_default()
    });
    state
        .bridge
        .send_and_wait("ExportComponent", payload, "Exported", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_save_component(
    state: State<'_, AgentState>,
    component_id: String,
    path: Option<String>,
) -> Result<Value, String> {
    let payload = serde_json::json!({
        "componentId": component_id,
        "path": path.unwrap_or_default()
    });
    state
        .bridge
        .send_and_wait("Save", payload, "Saved", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_load_component(
    state: State<'_, AgentState>,
    payload: Value,
) -> Result<Value, String> {
    state
        .bridge
        .send_and_wait("Load", payload, "Loaded", Duration::from_secs(10))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_get_template_types(
    state: State<'_, AgentState>,
    template_type: Option<String>,
) -> Result<Value, String> {
    let (payload, expected) = if let Some(t) = template_type {
        (serde_json::json!({ "templateType": t }), "TemplateTypeDetails")
    } else {
        (serde_json::json!({}), "TemplateTypes")
    };
    state
        .bridge
        .send_and_wait("GetTemplateTypes", payload, expected, Duration::from_secs(10))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_add_component_to_container(
    state: State<'_, AgentState>,
    container_id: String,
    child_id: String,
) -> Result<Value, String> {
    let payload = serde_json::json!({ "containerId": container_id, "childId": child_id });
    state
        .bridge
        .send_and_wait("AddComponentToContainer", payload, "Created", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn bridge_sync(state: State<'_, AgentState>) -> Result<Value, String> {
    state
        .bridge
        .send_and_wait("GetBridgeStatus", Value::Object(Default::default()), "BridgeStatus", Duration::from_secs(5))
        .await
        .map_err(|e| e.to_string())
}
