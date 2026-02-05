#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            use tauri_plugin_shell::ShellExt;

            let sidecar_command = app.shell().sidecar("codeflow-server").unwrap();
            let (mut rx, _child) = sidecar_command.spawn().expect("Failed to spawn sidecar");

            tauri::async_runtime::spawn(async move {
                use tauri_plugin_shell::process::CommandEvent;
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(line) => {
                            println!("[Go Backend] {}", String::from_utf8_lossy(&line));
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

