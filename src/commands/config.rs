use tauri::State;
use tracing::info;

use crate::config::{save_config, AgentConfig};
use crate::state::AgentState;

#[tauri::command]
pub async fn get_config(state: State<'_, AgentState>) -> Result<AgentConfig, String> {
    let config = state.config.lock().await;
    Ok(config.clone())
}

#[tauri::command]
pub async fn update_config(
    state: State<'_, AgentState>,
    config: AgentConfig,
) -> Result<(), String> {
    info!("Config update requested");
    {
        let mut current = state.config.lock().await;
        *current = config.clone();
    }
    save_config(&config).map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn get_agent_status(state: State<'_, AgentState>) -> Result<serde_json::Value, String> {
    let bridge_status = state.bridge.get_status().await;
    let watcher = state.watcher.lock().await;
    let watched = watcher.watched_paths();

    Ok(serde_json::json!({
        "running": true,
        "version": env!("CARGO_PKG_VERSION"),
        "watchedPaths": watched,
        "bridge": {
            "connected": bridge_status.connected,
            "authenticated": bridge_status.authenticated,
            "url": bridge_status.url
        }
    }))
}
