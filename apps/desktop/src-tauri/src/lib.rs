use std::sync::Arc;
use tauri::Manager;

/// Stores the backend port discovered from sidecar stdout.
pub struct BackendPort(pub Arc<std::sync::Mutex<Option<u16>>>);

#[tauri::command]
fn get_backend_port(state: tauri::State<BackendPort>) -> Option<u16> {
    state.0.lock().ok().and_then(|guard| *guard)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(BackendPort(Arc::new(std::sync::Mutex::new(None))))
        .invoke_handler(tauri::generate_handler![get_backend_port])
        .setup(|app| {
            use tauri_plugin_shell::ShellExt;

            let sidecar_command = match app.shell().sidecar("codeflow-server") {
                Ok(cmd) => cmd,
                Err(e) => {
                    eprintln!("[CodeFlow] Failed to create sidecar command: {}", e);
                    return Ok(());
                }
            };

            let (mut rx, _child) = match sidecar_command.spawn() {
                Ok(result) => result,
                Err(e) => {
                    eprintln!("[CodeFlow] Failed to spawn sidecar: {}", e);
                    return Ok(());
                }
            };

            let port_state = app.state::<BackendPort>().0.clone();

            tauri::async_runtime::spawn(async move {
                use tauri_plugin_shell::process::CommandEvent;
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(line) => {
                            let text = String::from_utf8_lossy(&line);
                            // Parse dynamic port from sidecar protocol
                            if let Some(port_str) = text.trim().strip_prefix("CODEFLOW_PORT:") {
                                if let Ok(port) = port_str.trim().parse::<u16>() {
                                    println!("[CodeFlow] Backend started on port {}", port);
                                    if let Ok(mut guard) = port_state.lock() {
                                        *guard = Some(port);
                                    }
                                }
                            } else {
                                println!("[Go Backend] {}", text);
                            }
                        }
                        CommandEvent::Stderr(line) => {
                            eprintln!("[Go Backend Error] {}", String::from_utf8_lossy(&line));
                        }
                        CommandEvent::Error(err) => {
                            eprintln!("[Go Backend] Error: {}", err);
                        }
                        CommandEvent::Terminated(status) => {
                            println!("[Go Backend] Terminated with status: {:?}", status);
                        }
                        _ => {}
                    }
                }
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
