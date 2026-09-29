(function() {
  const dropzone = document.getElementById('dropzone');
  const fileInput = document.getElementById('file-input');
  const progressBox = document.getElementById('progress-box');
  const progressBar = document.getElementById('progress-bar');
  const progressPercent = document.getElementById('progress-percent');
  const progressBytes = document.getElementById('progress-bytes');
  const progressSpeed = document.getElementById('progress-speed');
  const successBox = document.getElementById('success-box');
  const fileList = document.getElementById('file-list');
  const resetBtn = document.getElementById('reset-btn');
  const themeToggle = document.getElementById('theme-toggle');

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
  });

  function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function uploadFiles(files) {
    dropzone.style.display = 'none';
    successBox.style.display = 'none';
    progressBox.style.display = 'block';

    const formData = new FormData();
    for (let i = 0; i < files.length; i++) {
      formData.append('files', files[i]);
    }

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/upload', true);

    let startTime = Date.now();
    let prevLoaded = 0;
    let prevTime = startTime;

    xhr.upload.onprogress = function(e) {
      if (e.lengthComputable) {
        const percent = Math.round((e.loaded / e.total) * 100);
        progressBar.style.width = percent + '%';
        progressPercent.textContent = percent + '%';
        progressBytes.textContent = formatBytes(e.loaded) + ' / ' + formatBytes(e.total);

        // Speed calculation
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
      if (xhr.status === 200) {
        let results = [];
        try {
          results = JSON.parse(xhr.responseText);
        } catch (err) {
          // fallback
          for (let i = 0; i < files.length; i++) {
            results.push({ filename: files[i].name, bytes: files[i].size });
          }
        }
        showSuccess(results);
      } else {
        alert('Upload failed: ' + xhr.statusText);
        dropzone.style.display = 'block';
        progressBox.style.display = 'none';
      }
    };

    xhr.onerror = function() {
      alert('Network error while uploading');
      dropzone.style.display = 'block';
      progressBox.style.display = 'none';
    };

    xhr.send(formData);
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
