use anyhow::Result;
use base64::{engine::general_purpose::STANDARD as BASE64, Engine};
use notify::{RecommendedWatcher, RecursiveMode};
use notify_debouncer_mini::{new_debouncer, DebouncedEventKind, Debouncer};
use std::{
    collections::HashSet,
    path::{Path, PathBuf},
    time::Duration,
};
use tauri::{AppHandle, Emitter};
use tracing::{debug, error, warn};

use crate::events::FileEvent;

const FILE_EVENT_NAME: &str = "file-event";

pub struct WatcherService {
    debouncer: Option<Debouncer<RecommendedWatcher>>,
    watched_paths: HashSet<PathBuf>,
    file_types: Vec<String>,
    recursive: bool,
}

impl WatcherService {
    pub fn new(file_types: Vec<String>, recursive: bool) -> Self {
        Self {
            debouncer: None,
            watched_paths: HashSet::new(),
            file_types,
            recursive,
        }
    }

    /// Watch a path (file or directory). Emits initial events for existing files.
    pub fn watch(&mut self, app: &AppHandle, path: &str) -> Result<()> {
        let abs_path = std::fs::canonicalize(path).unwrap_or_else(|_| PathBuf::from(path));

        if self.watched_paths.contains(&abs_path) {
            debug!("Already watching: {}", abs_path.display());
            return Ok(());
        }

        // Lazily initialize the debouncer on first watch
        if self.debouncer.is_none() {
            let app_handle = app.clone();
            let file_types = self.file_types.clone();

            let debouncer = new_debouncer(
                Duration::from_millis(150),
                move |res: Result<Vec<notify_debouncer_mini::DebouncedEvent>, notify::Error>| {
                    match res {
                        Ok(events) => {
                            for event in events {
                                handle_debounced_event(&app_handle, &event, &file_types);
                            }
                        }
                        Err(e) => {
                            error!("Watcher error: {:?}", e);
                        }
                    }
                },
            )?;

            self.debouncer = Some(debouncer);
        }

        let debouncer = self.debouncer.as_mut().unwrap();
        let mode = if self.recursive {
            RecursiveMode::Recursive
        } else {
            RecursiveMode::NonRecursive
        };

        if abs_path.is_dir() {
            debouncer.watcher().watch(&abs_path, mode)?;
            self.watched_paths.insert(abs_path.clone());

            // Emit initial events for existing files in the directory
            let app_clone = app.clone();
            let file_types = self.file_types.clone();
            let path_clone = abs_path.clone();
            let recursive = self.recursive;
            tokio::task::spawn_blocking(move || {
                emit_initial_events(&app_clone, &path_clone, &file_types, recursive);
            });
        } else {
            // Watch the parent directory, track the specific file
            let parent = abs_path
                .parent()
                .ok_or_else(|| anyhow::anyhow!("No parent dir for {:?}", abs_path))?;
            debouncer
                .watcher()
                .watch(parent, RecursiveMode::NonRecursive)?;
            self.watched_paths.insert(abs_path.clone());

            // Emit initial event for the file immediately
            let app_clone = app.clone();
            let path_clone = abs_path.clone();
            tokio::task::spawn_blocking(move || {
                if let Some(name) = path_clone.file_name().and_then(|n| n.to_str()) {
                    emit_file_event(
                        &app_clone,
                        path_clone.to_string_lossy().as_ref(),
                        name,
                        "modify",
                    );
                }
            });
        }

        debug!("Now watching: {}", abs_path.display());
        Ok(())
    }

    /// Stop watching a path
    pub fn unwatch(&mut self, path: &str) -> Result<()> {
        let abs_path = std::fs::canonicalize(path).unwrap_or_else(|_| PathBuf::from(path));

        if !self.watched_paths.remove(&abs_path) {
            return Ok(());
        }

        if let Some(debouncer) = self.debouncer.as_mut() {
            if let Err(e) = debouncer.watcher().unwatch(&abs_path) {
                warn!("Failed to unwatch {}: {}", abs_path.display(), e);
            }
        }

        debug!("Stopped watching: {}", abs_path.display());
        Ok(())
    }

    /// Get list of currently watched paths
    pub fn watched_paths(&self) -> Vec<String> {
        self.watched_paths
            .iter()
            .map(|p| p.to_string_lossy().into_owned())
            .collect()
    }

    /// Stop watching all paths and drop the debouncer
    pub fn stop(&mut self) {
        self.debouncer = None;
        self.watched_paths.clear();
    }
}

fn is_relevant_file(path: &Path, file_types: &[String]) -> bool {
    if path.is_dir() {
        return false;
    }

    if file_types.is_empty() {
        return true;
    }

    let ext = path
        .extension()
        .and_then(|e| e.to_str())
        .map(|e| format!(".{}", e.to_lowercase()))
        .unwrap_or_default();

    file_types.iter().any(|ft| ft.to_lowercase() == ext)
}

fn emit_file_event(app: &AppHandle, path: &str, name: &str, operation: &str) {
    let content = if operation == "create" || operation == "modify" {
        read_file_with_retry(path)
    } else {
        None
    };

    let size = content
        .as_ref()
        .map(|c| BASE64.decode(c).map(|b| b.len() as u64).unwrap_or(0));

    let event = FileEvent {
        path: path.to_string(),
        name: name.to_string(),
        operation: operation.to_string(),
        content,
        size,
    };

    debug!("Emitting file event: {} ({})", path, operation);

    if let Err(e) = app.emit(FILE_EVENT_NAME, &event) {
        error!("Failed to emit file event: {}", e);
    }
}

fn read_file_with_retry(path: &str) -> Option<String> {
    for attempt in 0..5u32 {
        match std::fs::read(path) {
            Ok(bytes) => return Some(BASE64.encode(&bytes)),
            Err(e) => {
                let delay = std::time::Duration::from_millis(100 * (attempt as u64 + 1));
                debug!(
                    "File locked ({}) retry {}/5 in {:?}",
                    e,
                    attempt + 1,
                    delay
                );
                std::thread::sleep(delay);
            }
        }
    }
    error!("Failed to read file after 5 retries: {}", path);
    None
}

fn handle_debounced_event(
    app: &AppHandle,
    event: &notify_debouncer_mini::DebouncedEvent,
    file_types: &[String],
) {
    let path = &event.path;

    if !is_relevant_file(path, file_types) {
        return;
    }

    let path_str = path.to_string_lossy();
    let name = path.file_name().and_then(|n| n.to_str()).unwrap_or("");

    // DebouncedEventKind::Any covers Create/Modify; AnyContinuous is ongoing writes
    // We detect deletions by checking if the file no longer exists
    let operation = match event.kind {
        DebouncedEventKind::Any => {
            if path.exists() {
                "modify"
            } else {
                "delete"
            }
        }
        DebouncedEventKind::AnyContinuous => "modify",
        _ => "modify",
    };

    emit_file_event(app, &path_str, name, operation);
}

fn emit_initial_events(app: &AppHandle, dir: &Path, file_types: &[String], recursive: bool) {
    let walk = if recursive {
        walkdir_recursive(dir)
    } else {
        walkdir_flat(dir)
    };

    for path in walk {
        if is_relevant_file(&path, file_types) {
            let path_str = path.to_string_lossy();
            let name = path.file_name().and_then(|n| n.to_str()).unwrap_or("");
            emit_file_event(app, &path_str, name, "create");
        }
    }
}

fn walkdir_flat(dir: &Path) -> Vec<PathBuf> {
    std::fs::read_dir(dir)
        .map(|entries| {
            entries
                .filter_map(|e| e.ok())
                .map(|e| e.path())
                .filter(|p| p.is_file())
                .collect()
        })
        .unwrap_or_default()
}

fn walkdir_recursive(dir: &Path) -> Vec<PathBuf> {
    let mut result = Vec::new();
    if let Ok(entries) = std::fs::read_dir(dir) {
        for entry in entries.filter_map(|e| e.ok()) {
            let path = entry.path();
            if path.is_file() {
                result.push(path);
            } else if path.is_dir() {
                result.extend(walkdir_recursive(&path));
            }
        }
    }
    result
}
