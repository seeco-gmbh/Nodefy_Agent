use tauri::{AppHandle, State};
use tracing::{info, warn};

use crate::state::AgentState;

#[tauri::command]
pub async fn watch_path(
    app: AppHandle,
    state: State<'_, AgentState>,
    path: String,
) -> Result<(), String> {
    info!("Watch path requested: {}", path);
    let mut watcher = state.watcher.lock().await;
    watcher.watch(&app, &path).map_err(|e| {
        warn!("Failed to watch {}: {}", path, e);
        e.to_string()
    })
}

#[tauri::command]
pub async fn unwatch_path(
    state: State<'_, AgentState>,
    path: String,
) -> Result<(), String> {
    info!("Unwatch path requested: {}", path);

    let mut watcher = state.watcher.lock().await;
    watcher.unwatch(&path).map_err(|e| {
        warn!("Failed to unwatch {}: {}", path, e);
        e.to_string()
    })
}

#[tauri::command]
pub async fn get_watched_paths(state: State<'_, AgentState>) -> Result<Vec<String>, String> {
    let watcher = state.watcher.lock().await;
    Ok(watcher.watched_paths())
}
