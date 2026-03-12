pub mod commands;
pub mod config;
pub mod events;
pub mod services;
pub mod state;

use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    Manager, Runtime,
};
use tracing::info;

use commands::{
    bridge::*,
    config::*,
    files::*,
    watcher::*,
};
use state::AgentState;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info".into()),
        )
        .init();

    let agent_config = config::load_config();
    info!("Loaded agent config: {:?}", agent_config);

    let agent_state = AgentState::new(agent_config);

    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .manage(agent_state)
        .setup(|app| {
            #[cfg(target_os = "macos")]
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            setup_tray(app)?;
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            // Watcher commands
            watch_path,
            unwatch_path,
            get_watched_paths,
            // File commands
            read_file,
            open_file_dialog,
            save_dialog,
            save_file,
            load_dialog,
            import_file,
            // Bridge commands
            bridge_connect,
            bridge_disconnect,
            bridge_status,
            bridge_send,
            bridge_create_component,
            bridge_update_component,
            bridge_delete_component,
            bridge_get_component_info,
            bridge_execute,
            bridge_warmup,
            bridge_connect_ports,
            bridge_add_port,
            bridge_delete_port,
            bridge_update_port,
            bridge_get_connection_info,
            bridge_export_component,
            bridge_save_component,
            bridge_load_component,
            bridge_get_template_types,
            bridge_add_component_to_container,
            bridge_sync,
            // Config commands
            get_config,
            update_config,
            get_agent_status,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

fn setup_tray<R: Runtime>(app: &mut tauri::App<R>) -> Result<(), Box<dyn std::error::Error>> {
    let status = MenuItem::with_id(app, "status", "Nodefy Agent — Running", false, None::<&str>)?;
    let separator = tauri::menu::PredefinedMenuItem::separator(app)?;
    let quit = MenuItem::with_id(app, "quit", "Quit Nodefy", true, None::<&str>)?;

    let menu = Menu::with_items(app, &[&status, &separator, &quit])?;

    let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/tray_icon.png"))?;

    let _tray = TrayIconBuilder::new()
        .icon(icon)
        .icon_as_template(true)
        .menu(&menu)
        .show_menu_on_left_click(true)
        .tooltip("Nodefy Agent")
        .on_menu_event(|app, event| {
            if event.id().as_ref() == "quit" {
                info!("Quit requested from tray menu");
                let app = app.clone();
                tauri::async_runtime::spawn(async move {
                    graceful_shutdown(&app).await;
                    app.exit(0);
                });
            }
        })
        .build(app)?;

    Ok(())
}

async fn graceful_shutdown<R: Runtime>(app: &tauri::AppHandle<R>) {
    if let Some(state) = app.try_state::<AgentState>() {
        info!("Disconnecting from Adapt Bridge...");
        let _ = state.bridge.disconnect().await;

        info!("Stopping file watcher...");
        if let Ok(mut watcher) = state.watcher.try_lock() {
            watcher.stop();
        }
    }
    info!("Nodefy Agent shutdown complete");
}
