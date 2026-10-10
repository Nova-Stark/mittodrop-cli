(function() {
  const dropzone = document.getElementById('dropzone');
  const fileInput = document.getElementById('file-input');
  const progressBox = document.getElementById('progress-box');
  const progressBar = document.getElementById('progress-bar');
  const progressPercent = document.getElementById('progress-percent');
  const progressBytes = document.getElementById('progress-bytes');
  const progressSpeed = document.getElementById('progress-speed');
  const progressStatus = document.getElementById('progress-status');
  const successBox = document.getElementById('success-box');
  const fileList = document.getElementById('file-list');
  const resetBtn = document.getElementById('reset-btn');
  const themeToggle = document.getElementById('theme-toggle');
  const tokenInput = document.getElementById('token-input');
  const tokenToggle = document.getElementById('token-toggle');
  const tokenBanner = document.getElementById('token-banner');

  // Load token from URL query or sessionStorage
  const urlParams = new URLSearchParams(window.location.search);
  const urlToken = urlParams.get('token');
  if (urlToken) {
    if (tokenInput) tokenInput.value = urlToken;
    try { sessionStorage.setItem('mittodrop-token', urlToken); } catch (_) {}
  } else {
    try {
      const savedToken = sessionStorage.getItem('mittodrop-token');
      if (savedToken && tokenInput) tokenInput.value = savedToken;
    } catch (_) {}
  }

  if (tokenInput) {
    tokenInput.addEventListener('input', () => {
      try { sessionStorage.setItem('mittodrop-token', tokenInput.value.trim()); } catch (_) {}
      if (tokenBanner) tokenBanner.style.display = 'none';
      tokenInput.classList.remove('input-error');
    });
  }

  if (tokenToggle && tokenInput) {
    tokenToggle.addEventListener('click', () => {
      tokenInput.type = tokenInput.type === 'password' ? 'text' : 'password';
    });
  }

  // Theme management
  const savedTheme = localStorage.getItem('mittodrop-theme') || 'dark';
  document.documentElement.setAttribute('data-theme', savedTheme);

  if (themeToggle) {
    themeToggle.addEventListener('click', () => {
      const current = document.documentElement.getAttribute('data-theme');
      const next = current === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('mittodrop-theme', next);
    });
  }

  // Click dropzone to open file picker
  dropzone.addEventListener('click', () => fileInput.click());

  // Drag and drop events
  ['dragenter', 'dragover'].forEach(eventName => {
    dropzone.addEventListener(eventName, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropzone.classList.add('dragover');
    });
  });

  ['dragleave', 'drop'].forEach(eventName => {
    dropzone.addEventListener(eventName, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropzone.classList.remove('dragover');
    });
  });

  dropzone.addEventListener('drop', (e) => {
    const files = e.dataTransfer.files;
    if (files && files.length > 0) {
      uploadFiles(files);
    }
  });

  fileInput.addEventListener('change', () => {
    if (fileInput.files && fileInput.files.length > 0) {
      uploadFiles(fileInput.files);
    }
  });

  resetBtn.addEventListener('click', () => {
    fileInput.value = '';
    successBox.style.display = 'none';
    progressBox.style.display = 'none';
    dropzone.style.display = 'block';
    if (tokenBanner) tokenBanner.style.display = 'none';
    if (tokenInput) tokenInput.classList.remove('input-error');
  });

  function formatBytes(bytes) {
    if (!bytes || bytes <= 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  // Extensions of files that are already compressed; compressing again wastes CPU
  const PRECOMPRESSED_EXTS = new Set([
    'zip', 'gz', 'tgz', 'bz2', 'xz', '7z', 'rar', 'zst',
    'jpg', 'jpeg', 'png', 'gif', 'webp', 'avif',
    'mp4', 'mkv', 'avi', 'mov', 'webm', 'mp3', 'flac', 'aac', 'wav', 'ogg',
    'pdf', 'docx', 'xlsx', 'pptx', 'apk', 'iso', 'dmg', 'exe'
  ]);

  function getExtension(name) {
    const idx = name.lastIndexOf('.');
    return idx !== -1 ? name.slice(idx + 1).toLowerCase() : '';
  }

  function shouldCompress(file) {
    if (typeof CompressionStream === 'undefined') return false;
    // Don't compress tiny files (< 1 KB) or very large files (> 200 MB) in-browser
    if (file.size < 1024 || file.size > 200 * 1024 * 1024) return false;
    const ext = getExtension(file.name);
    return !PRECOMPRESSED_EXTS.has(ext);
  }

  // Compute SHA-256 using Web Crypto API when available (secure contexts & <= 256 MB)
  async function computeSHA256(file) {
    if (!window.crypto || !window.crypto.subtle) return '';
    if (file.size > 256 * 1024 * 1024) return '';
    try {
      const buffer = await file.arrayBuffer();
      const hashBuffer = await crypto.subtle.digest('SHA-256', buffer);
      const hashArray = Array.from(new Uint8Array(hashBuffer));
      return hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
    } catch (e) {
      console.warn('Web Crypto SHA-256 calculation skipped:', e);
      return '';
    }
  }

  // Adaptive browser gzip compression using native CompressionStream
  async function compressFileGzip(file) {
    try {
      const cs = new CompressionStream('gzip');
      const compressedStream = file.stream().pipeThrough(cs);
      const compressedBlob = await new Response(compressedStream).blob();
      // Only keep compressed blob if it achieved at least 5% compression
      if (compressedBlob.size < file.size * 0.95) {
        return compressedBlob;
      }
      return null;
    } catch (e) {
      console.warn('Browser gzip compression failed, falling back to raw:', e);
      return null;
    }
  }

  function uploadSingleFile(file, compressedBlob, checksum, currentIdx, totalFiles) {
    return new Promise((resolve, reject) => {
      const bodyToSend = compressedBlob || file;
      const isCompressed = !!compressedBlob;
      const currentToken = tokenInput ? tokenInput.value.trim() : (urlToken || '');
      const uploadUrl = currentToken ? `/upload?token=${encodeURIComponent(currentToken)}` : '/upload';

      const xhr = new XMLHttpRequest();
      xhr.open('POST', uploadUrl, true);

      xhr.setRequestHeader('Content-Type', 'application/octet-stream');
      xhr.setRequestHeader('X-File-Name', encodeURIComponent(file.name));
      xhr.setRequestHeader('X-File-Size', file.size.toString());
      if (currentToken) {
        xhr.setRequestHeader('X-LinkShare-Token', currentToken);
      }
      if (checksum) {
        xhr.setRequestHeader('X-File-Checksum', checksum);
      }
      if (isCompressed) {
        xhr.setRequestHeader('Content-Encoding', 'gzip');
        xhr.setRequestHeader('X-Content-Encoding', 'gzip');
      }

      const fileLabel = totalFiles > 1 ? `[${currentIdx + 1}/${totalFiles}] ` : '';
      const compTag = isCompressed ? ' (gzip)' : '';

      let startTime = Date.now();
      let prevLoaded = 0;
      let prevTime = startTime;

      xhr.upload.onprogress = function(e) {
        if (e.lengthComputable) {
          const percent = Math.round((e.loaded / e.total) * 100);
          progressBar.style.width = percent + '%';
          progressPercent.textContent = percent + '%';
          if (progressStatus) {
            progressStatus.textContent = `${fileLabel}Uploading ${file.name}${compTag}...`;
          }
          progressBytes.textContent = formatBytes(e.loaded) + ' / ' + formatBytes(e.total);

          const now = Date.now();
          const timeDiff = (now - prevTime) / 1000;
          if (timeDiff >= 0.5) {
            const speed = (e.loaded - prevLoaded) / timeDiff;
            progressSpeed.textContent = formatBytes(speed) + '/s';
            prevLoaded = e.loaded;
            prevTime = now;
          }
        }
      };

      xhr.onload = function() {
        if (xhr.status >= 200 && xhr.status < 300) {
          if (tokenBanner) tokenBanner.style.display = 'none';
          if (tokenInput) tokenInput.classList.remove('input-error');
          try {
            const parsed = JSON.parse(xhr.responseText);
            resolve(parsed && parsed.length > 0 ? parsed[0] : { filename: file.name, bytes: file.size });
          } catch (_) {
            resolve({ filename: file.name, bytes: file.size });
          }
        } else {
          if (xhr.status === 401) {
            if (tokenBanner) {
              tokenBanner.textContent = 'Unauthorized: Invalid or missing access token. Please enter valid token above.';
              tokenBanner.style.display = 'block';
            }
            if (tokenInput) {
              tokenInput.classList.add('input-error');
              tokenInput.focus();
            }
          }
          reject(new Error(xhr.responseText || `HTTP ${xhr.status}: ${xhr.statusText}`));
        }
      };

      xhr.onerror = function() {
        reject(new Error('Network connection error during upload'));
      };

      xhr.send(bodyToSend);
    });
  }

  async function uploadFiles(files) {
    if (!files || files.length === 0) return;

    dropzone.style.display = 'none';
    successBox.style.display = 'none';
    progressBox.style.display = 'block';

    const results = [];
    const total = files.length;

    try {
      for (let i = 0; i < total; i++) {
        const file = files[i];
        const fileLabel = total > 1 ? `[${i + 1}/${total}] ` : '';

        // Reset progress indicators for this file
        progressBar.style.width = '0%';
        progressPercent.textContent = '0%';
        progressSpeed.textContent = '-- MB/s';
        progressBytes.textContent = `0 B / ${formatBytes(file.size)}`;

        // Step 1: Web Crypto SHA-256 Checksum
        if (progressStatus) {
          progressStatus.textContent = `${fileLabel}Hashing ${file.name}...`;
        }
        const checksum = await computeSHA256(file);

        // Step 2: Adaptive Gzip Compression
        let compressedBlob = null;
        if (shouldCompress(file)) {
          if (progressStatus) {
            progressStatus.textContent = `${fileLabel}Compressing ${file.name} (gzip)...`;
          }
          compressedBlob = await compressFileGzip(file);
        }

        // Step 3: Stream payload to /upload
        if (progressStatus) {
          const compTag = compressedBlob ? ' (gzip)' : '';
          progressStatus.textContent = `${fileLabel}Uploading ${file.name}${compTag}...`;
        }

        const res = await uploadSingleFile(file, compressedBlob, checksum, i, total);
        results.push(res);
      }

      showSuccess(results);
    } catch (err) {
      const is401 = err.message && (err.message.includes('401') || err.message.toLowerCase().includes('unauthorized'));
      if (!is401) {
        alert('Upload failed: ' + err.message);
      }
      dropzone.style.display = 'block';
      progressBox.style.display = 'none';
    }
  }

  function showSuccess(results) {
    progressBox.style.display = 'none';
    fileList.innerHTML = '';

    results.forEach(file => {
      const li = document.createElement('li');
      li.className = 'file-item';
      li.innerHTML = `<span>${file.filename}</span><span style="color:var(--text-muted)">${formatBytes(file.bytes)}</span>`;
      fileList.appendChild(li);
    });

    successBox.style.display = 'block';
  }
})();
