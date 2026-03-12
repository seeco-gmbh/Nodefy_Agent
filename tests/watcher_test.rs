use std::time::Duration;
use tempfile::TempDir;

/// Integration test for the file watcher using temp directory.
/// These tests require tokio runtime.
#[tokio::test]
async fn test_watcher_relevant_file_detection() {
    // Test extension filtering logic
    let file_types = vec![".csv".to_string(), ".json".to_string()];

    let is_relevant = |path: &str| -> bool {
        let ext = std::path::Path::new(path)
            .extension()
            .and_then(|e| e.to_str())
            .map(|e| format!(".{}", e.to_lowercase()))
            .unwrap_or_default();

        if file_types.is_empty() {
            return true;
        }
        file_types.iter().any(|ft| ft.to_lowercase() == ext)
    };

    assert!(is_relevant("data.csv"));
    assert!(is_relevant("config.json"));
    assert!(!is_relevant("image.png"));
    assert!(!is_relevant("document.pdf"));
    assert!(!is_relevant("noextension"));
}

#[tokio::test]
async fn test_watcher_creates_temp_dir() {
    let dir = TempDir::new().unwrap();
    assert!(dir.path().exists());
    assert!(dir.path().is_dir());
}

#[test]
fn test_base64_file_encoding() {
    use base64::{engine::general_purpose::STANDARD as BASE64, Engine};

    let content = b"hello,world\n1,2\n3,4\n";
    let encoded = BASE64.encode(content);
    let decoded = BASE64.decode(&encoded).unwrap();

    assert_eq!(decoded, content);
}

#[test]
fn test_walkdir_flat() {
    let dir = TempDir::new().unwrap();

    // Create test files
    std::fs::write(dir.path().join("data.csv"), "a,b").unwrap();
    std::fs::write(dir.path().join("notes.txt"), "notes").unwrap();
    std::fs::create_dir(dir.path().join("subdir")).unwrap();
    std::fs::write(dir.path().join("subdir").join("nested.csv"), "c,d").unwrap();

    // Flat walk should only return files in the root
    let files: Vec<_> = std::fs::read_dir(dir.path())
        .unwrap()
        .filter_map(|e| e.ok())
        .map(|e| e.path())
        .filter(|p| p.is_file())
        .collect();

    assert_eq!(files.len(), 2); // only data.csv and notes.txt, not nested.csv
}
