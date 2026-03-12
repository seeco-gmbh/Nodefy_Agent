use nodefy_agent_lib::events::{
    AgentStatus, BridgeEvent, BridgeStatus, DialogResult, FileContent, FileEvent, ImportResult,
    SaveResult,
};

#[test]
fn test_file_event_serialization() {
    let event = FileEvent {
        path: "/home/user/data.csv".to_string(),
        name: "data.csv".to_string(),
        operation: "create".to_string(),
        content: Some("dGVzdA==".to_string()),
        size: Some(4),
    };

    let json = serde_json::to_value(&event).unwrap();
    assert_eq!(json["path"], "/home/user/data.csv");
    assert_eq!(json["name"], "data.csv");
    assert_eq!(json["operation"], "create");
    assert_eq!(json["content"], "dGVzdA==");
    assert_eq!(json["size"], 4);
}

#[test]
fn test_file_event_without_content() {
    let event = FileEvent {
        path: "/home/user/data.csv".to_string(),
        name: "data.csv".to_string(),
        operation: "delete".to_string(),
        content: None,
        size: None,
    };

    let json = serde_json::to_value(&event).unwrap();
    // None fields should be absent (skip_serializing_if = "Option::is_none")
    assert!(!json.as_object().unwrap().contains_key("content"));
    assert!(!json.as_object().unwrap().contains_key("size"));
}

#[test]
fn test_bridge_status_serialization() {
    let status = BridgeStatus {
        connected: true,
        authenticated: true,
        url: "ws://localhost:9090".to_string(),
    };

    let json = serde_json::to_value(&status).unwrap();
    assert_eq!(json["connected"], true);
    assert_eq!(json["authenticated"], true);
    assert_eq!(json["url"], "ws://localhost:9090");
}

#[test]
fn test_dialog_result_cancelled() {
    let result = DialogResult {
        cancelled: true,
        path: None,
        name: None,
        size: None,
        content: None,
    };

    let json = serde_json::to_value(&result).unwrap();
    assert_eq!(json["cancelled"], true);
    assert!(!json.as_object().unwrap().contains_key("path"));
}

#[test]
fn test_save_result_serialization() {
    let result = SaveResult {
        cancelled: false,
        path: Some("/home/user/project.ndf".to_string()),
        bytes: Some(1024),
    };

    let json = serde_json::to_value(&result).unwrap();
    assert_eq!(json["cancelled"], false);
    assert_eq!(json["path"], "/home/user/project.ndf");
    assert_eq!(json["bytes"], 1024);
}

#[test]
fn test_camel_case_serialization() {
    // Verify serde rename_all = "camelCase" is applied
    let status = AgentStatus {
        running: true,
        version: "0.2.0".to_string(),
        watched_paths: vec!["/home/user".to_string()],
        bridge: BridgeStatus {
            connected: false,
            authenticated: false,
            url: String::new(),
        },
    };

    let json = serde_json::to_value(&status).unwrap();
    // camelCase: watched_paths → watchedPaths
    assert!(json.as_object().unwrap().contains_key("watchedPaths"));
    assert!(!json.as_object().unwrap().contains_key("watched_paths"));
}
