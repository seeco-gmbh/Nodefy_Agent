use base64::{engine::general_purpose::STANDARD as BASE64, Engine};
use std::path::{Path, PathBuf};
use tauri::AppHandle;
use tauri_plugin_dialog::DialogExt;
use tracing::{info, warn};

use crate::events::{DialogResult, FileContent, ImportResult, SaveResult};

/// Read a file and return its content as base64
#[tauri::command]
pub async fn read_file(path: String) -> Result<FileContent, String> {
    tokio::task::spawn_blocking(move || {
        let abs_path = std::fs::canonicalize(&path).unwrap_or_else(|_| PathBuf::from(&path));
        let metadata =
            std::fs::metadata(&abs_path).map_err(|e| format!("File not found: {}", e))?;
        let content = read_with_retry(&abs_path, 5).map_err(|e| e.to_string())?;
        let encoded = BASE64.encode(&content);
        let name = abs_path
            .file_name()
            .and_then(|n| n.to_str())
            .unwrap_or("")
            .to_string();

        Ok(FileContent {
            path: abs_path.to_string_lossy().into_owned(),
            name,
            size: metadata.len(),
            content: encoded,
        })
    })
    .await
    .map_err(|e| e.to_string())?
}

/// Open native file dialog and return selected file content
#[tauri::command]
pub async fn open_file_dialog(
    app: AppHandle,
    title: Option<String>,
    filters: Option<Vec<String>>,
) -> Result<DialogResult, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    let title_str = title.unwrap_or_else(|| "Select a file".to_string());

    let mut builder = app.dialog().file().set_title(&title_str);

    if let Some(ref exts) = filters {
        let clean: Vec<String> = exts
            .iter()
            .map(|s| s.trim_start_matches('.').to_string())
            .collect();
        let refs: Vec<&str> = clean.iter().map(|s| s.as_str()).collect();
        if !refs.is_empty() {
            builder = builder.add_filter("Files", &refs);
        }
    }

    builder.pick_file(move |path| {
        let _ = tx.send(path);
    });

    let file_path = rx.await.map_err(|e| e.to_string())?;

    match file_path {
        None => Ok(DialogResult {
            cancelled: true,
            path: None,
            name: None,
            size: None,
            content: None,
        }),
        Some(fp) => {
            let path_str = fp.to_string();
            tokio::task::spawn_blocking(move || {
                let p = PathBuf::from(&path_str);
                let metadata = std::fs::metadata(&p).map_err(|e| e.to_string())?;
                let bytes = read_with_retry(&p, 5).map_err(|e| e.to_string())?;
                let content = BASE64.encode(&bytes);
                let name = p
                    .file_name()
                    .and_then(|n| n.to_str())
                    .unwrap_or("")
                    .to_string();

                info!("File selected: {} ({} bytes)", path_str, metadata.len());
                Ok::<DialogResult, String>(DialogResult {
                    cancelled: false,
                    path: Some(path_str),
                    name: Some(name),
                    size: Some(metadata.len()),
                    content: Some(content),
                })
            })
            .await
            .map_err(|e| e.to_string())?
        }
    }
}

/// Open native save dialog and return chosen path
#[tauri::command]
pub async fn save_dialog(
    app: AppHandle,
    default_name: Option<String>,
    filters: Option<Vec<String>>,
) -> Result<DialogResult, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    let default_name = default_name.unwrap_or_else(|| "project.ndf".to_string());

    let mut builder = app
        .dialog()
        .file()
        .set_title("Save Project")
        .set_file_name(&default_name);

    let exts = filters.unwrap_or_else(|| vec!["ndf".into(), "json".into()]);
    let clean: Vec<String> = exts
        .iter()
        .map(|s| s.trim_start_matches('.').to_string())
        .collect();
    let refs: Vec<&str> = clean.iter().map(|s| s.as_str()).collect();
    if !refs.is_empty() {
        builder = builder.add_filter("Files", &refs);
    }

    builder.save_file(move |path| {
        let _ = tx.send(path);
    });

    let result = rx.await.map_err(|e| e.to_string())?;

    match result {
        None => Ok(DialogResult {
            cancelled: true,
            path: None,
            name: None,
            size: None,
            content: None,
        }),
        Some(p) => {
            let path_str = p.to_string();
            let name = PathBuf::from(&path_str)
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("")
                .to_string();
            Ok(DialogResult {
                cancelled: false,
                path: Some(path_str),
                name: Some(name),
                size: None,
                content: None,
            })
        }
    }
}

/// Save JSON data to a file (show dialog if path is not provided)
#[tauri::command]
pub async fn save_file(
    app: AppHandle,
    path: Option<String>,
    data: serde_json::Value,
    default_name: Option<String>,
) -> Result<SaveResult, String> {
    let save_path = match path.filter(|p| !p.is_empty()) {
        Some(p) => p,
        None => {
            // Open save dialog inline
            let (tx, rx) = tokio::sync::oneshot::channel::<Option<String>>();
            let dn = default_name
                .clone()
                .unwrap_or_else(|| "project.ndf".to_string());

            app.dialog()
                .file()
                .set_title("Save Project")
                .set_file_name(&dn)
                .add_filter("Files", &["ndf", "json"])
                .save_file(move |p| {
                    let _ = tx.send(p.map(|fp| fp.to_string()));
                });

            match rx.await.map_err(|e| e.to_string())? {
                None => {
                    return Ok(SaveResult {
                        cancelled: true,
                        path: None,
                        bytes: None,
                    })
                }
                Some(p) => p,
            }
        }
    };

    let save_path = ensure_extension(&save_path, &["ndf", "json"], "ndf");

    let content = serde_json::to_string_pretty(&data)
        .map_err(|e| format!("Failed to serialize data: {}", e))?;

    tokio::task::spawn_blocking({
        let save_path = save_path.clone();
        let content = content.clone();
        move || {
            let dir = PathBuf::from(&save_path)
                .parent()
                .map(|p| p.to_owned())
                .unwrap_or_else(|| PathBuf::from("."));
            std::fs::create_dir_all(&dir)
                .map_err(|e| format!("Failed to create directory: {}", e))?;
            std::fs::write(&save_path, &content)
                .map_err(|e| format!("Failed to write file: {}", e))?;
            Ok::<(), String>(())
        }
    })
    .await
    .map_err(|e| e.to_string())??;

    info!("File saved: {} ({} bytes)", save_path, content.len());

    Ok(SaveResult {
        cancelled: false,
        path: Some(save_path),
        bytes: Some(content.len()),
    })
}

/// Open file dialog and load JSON content
#[tauri::command]
pub async fn load_dialog(
    app: AppHandle,
    filters: Option<Vec<String>>,
) -> Result<serde_json::Value, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();

    let mut builder = app
        .dialog()
        .file()
        .set_title("Open Project")
        .add_filter("Nodefy Files", &["ndf", "json"]);

    if let Some(ref exts) = filters {
        let clean: Vec<String> = exts
            .iter()
            .map(|s| s.trim_start_matches('.').to_string())
            .collect();
        let refs: Vec<&str> = clean.iter().map(|s| s.as_str()).collect();
        if !refs.is_empty() {
            builder = builder.add_filter("Custom", &refs);
        }
    }

    builder.pick_file(move |path| {
        let _ = tx.send(path);
    });

    let file_path = rx.await.map_err(|e| e.to_string())?;

    match file_path {
        None => Err("cancelled".to_string()),
        Some(fp) => {
            let path_str = fp.to_string();
            tokio::task::spawn_blocking(move || {
                let content = std::fs::read_to_string(&path_str)
                    .map_err(|e| format!("Failed to read file: {}", e))?;
                let name = PathBuf::from(&path_str)
                    .file_name()
                    .and_then(|n| n.to_str())
                    .unwrap_or("")
                    .to_string();

                let data: serde_json::Value = serde_json::from_str(&content)
                    .map_err(|e| format!("File is not valid JSON: {}", e))?;

                info!("File loaded: {}", path_str);

                Ok::<serde_json::Value, String>(serde_json::json!({
                    "cancelled": false,
                    "data": data,
                    "fileName": name,
                    "path": path_str
                }))
            })
            .await
            .map_err(|e| e.to_string())?
        }
    }
}

/// Import JSON file (dialog + parse)
#[tauri::command]
pub async fn import_file(app: AppHandle) -> Result<ImportResult, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();

    app.dialog()
        .file()
        .set_title("Import File")
        .add_filter("JSON Files", &["json"])
        .pick_file(move |path| {
            let _ = tx.send(path);
        });

    let file_path = rx.await.map_err(|e| e.to_string())?;

    match file_path {
        None => Err("cancelled".to_string()),
        Some(fp) => {
            let path_str = fp.to_string();
            tokio::task::spawn_blocking(move || {
                let content = std::fs::read_to_string(&path_str)
                    .map_err(|e| format!("Failed to read file: {}", e))?;
                let name = PathBuf::from(&path_str)
                    .file_name()
                    .and_then(|n| n.to_str())
                    .unwrap_or("upload.json")
                    .to_string();

                let data: serde_json::Value = serde_json::from_str(&content)
                    .map_err(|e| format!("File is not valid JSON: {}", e))?;

                Ok::<ImportResult, String>(ImportResult {
                    data,
                    file_name: name,
                })
            })
            .await
            .map_err(|e| e.to_string())?
        }
    }
}

fn read_with_retry(path: &Path, max_retries: u32) -> anyhow::Result<Vec<u8>> {
    let mut last_err = None;
    for i in 0..max_retries {
        match std::fs::read(path) {
            Ok(bytes) => return Ok(bytes),
            Err(e) => {
                let delay = std::time::Duration::from_millis(100 * (i as u64 + 1));
                warn!(
                    "File locked ({}) retry {}/{} in {:?}",
                    e,
                    i + 1,
                    max_retries,
                    delay
                );
                std::thread::sleep(delay);
                last_err = Some(e);
            }
        }
    }
    Err(last_err.unwrap().into())
}

fn ensure_extension(path: &str, valid_exts: &[&str], default_ext: &str) -> String {
    let p = PathBuf::from(path);
    let ext = p
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_lowercase();
    if valid_exts.iter().any(|v| *v == ext) {
        path.to_string()
    } else {
        format!("{}.{}", path, default_ext)
    }
}
