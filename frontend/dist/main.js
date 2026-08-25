const state = {
  profile: null,
  currentPage: "first-run",
  running: false,
  completed: false,
  networkReady: false,
  checkingNetwork: true,
  phases: {
    "first-run": { state: "idle", message: "The mandatory setup has not started yet." },
    upgrade: { state: "idle", message: "Caracal update has not started yet." },
    finish: { state: "idle", message: "Reboot becomes available after setup finishes." },
  },
  launchCompleted: false,
  currentImage: "",
};
const elements = {
  stepItems: [...document.querySelectorAll(".step-chip")],
  progressCards: [...document.querySelectorAll(".progress-item")],
  pages: {
    "first-run": document.querySelector("#page-first-run"),
    switcher: document.querySelector("#page-switcher"),
    finish: document.querySelector("#page-finish"),
  },
  upgradeButton: document.querySelector("#upgrade-button"),
  runButton: document.querySelector("#run-button"),
  networkBanner: document.querySelector("#network-banner"),
  rebootButton: document.querySelector("#reboot-button"),
  finishSummary: document.querySelector("#finish-summary"),
  log: document.querySelector("#log"),
  runPill: document.querySelector("#run-pill"),
  switcherPill: document.querySelector("#switcher-pill"),
  imageGrid: document.querySelector("#image-grid"),
  sameImageBanner: document.querySelector("#same-image-banner"),
  rebaseDetails: document.querySelector("#rebase-details"),
  rebaseTargetName: document.querySelector("#rebase-target-name"),
  switchButton: document.querySelector("#switch-button"),
  switcherLog: document.querySelector("#switcher-log"),
};

function backend() {
  const bound = window.go?.guiapp?.App || window.go?.main?.App;
  if (!bound) {
    throw new Error("Wails backend bindings are not available.");
  }
  return bound;
}

async function boot() {
  bindEvents();
  bindRuntimeEvents();
  await loadProfile();

  state.launchCompleted = await backend().HasLaunchCompleted();
  if (state.launchCompleted) {
    await loadSwitcher();
    render();
    return;
  }

  await refreshNetwork();
  window.setInterval(refreshNetwork, 10000);
  render();
  appendLog("Wizard ready.");
}

async function loadSwitcher() {
  state.currentImage = await backend().GetCurrentImageName();
  state.availableImages = await backend().GetAvailableImages();

  const firstRecommended = state.availableImages.find(
    (img) => img.recommended && img.imageName !== state.currentImage
  );
  if (firstRecommended) {
    state.selectedImage = firstRecommended.imageName;
  } else if (state.availableImages.length > 0) {
    const notCurrent = state.availableImages.find((img) => img.imageName !== state.currentImage);
    state.selectedImage = notCurrent ? notCurrent.imageName : state.availableImages[0].imageName;
  }

  state.currentPage = "switcher";
  renderSwitcher();
  appendSwitcherLog("Version switcher loaded.");
}

function appendSwitcherLog(message) {
  const line = `[${new Date().toLocaleTimeString()}] ${message}`;
  elements.switcherLog.textContent = elements.switcherLog.textContent
    ? `${elements.switcherLog.textContent}\n${line}`
    : line;
  elements.switcherLog.scrollTop = elements.switcherLog.scrollHeight;
}

function bindEvents() {
  elements.runButton.addEventListener("click", async () => {
    if (state.running || !state.networkReady) {
      return;
    }

    state.running = true;
    state.completed = false;
    state.phases["first-run"] = {
      state: "idle",
      message: "The terminal window will open shortly.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Reboot will unlock after the mandatory setup finishes.",
    };
    render();

    appendLog("Starting mandatory setup...");

    try {
      const result = await backend().RunSetup({});
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `First-run setup completed for ${result.appliedUsername} on ${result.appliedHostname}. Reboot now to finish applying the Caracal session changes.`;
      await loadProfile();
      appendLog("Mandatory setup finished. Reboot is ready.");
      render();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.upgradeButton.addEventListener("click", async () => {
    if (state.running || !state.networkReady) {
      return;
    }

    state.running = true;
    state.completed = false;
    state.phases["first-run"] = {
      state: "idle",
      message: "First-run setup was not requested.",
    };
    state.phases.upgrade = {
      state: "idle",
      message: "The terminal window will open for the Caracal update.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Reboot will be available after the update finishes.",
    };
    render();

    appendLog("Starting Caracal update...");

    try {
      const result = await backend().RunUpgrade();
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `Caracal update completed for ${result.appliedUsername} on ${result.appliedHostname}. Reboot if the updater requested it.`;
      await loadProfile();
      appendLog("Caracal update finished.");
      render();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.rebootButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    state.running = true;
    render();
    appendLog("Requesting reboot...");

    try {
      await backend().RebootNow();
    } catch (error) {
      state.running = false;
      appendLog(error?.message || String(error));
      render();
    }
  });

  elements.switchButton.addEventListener("click", async () => {
    if (state.running || !state.selectedImage) {
      return;
    }

    const chosen = state.availableImages.find((img) => img.imageName === state.selectedImage);
    if (!chosen) {
      return;
    }

    if (chosen.imageName === state.currentImage) {
      return;
    }

    const confirmMsg = `Switch to ${chosen.label}?\n\nThis will run:\nrpm-ostree rebase ostree-unverified-registry:ghcr.io/caracal-dev/${chosen.imageName}:latest\n\nThe new image will be used on the next boot. Are you sure?`;
    const confirmed = window.confirm(confirmMsg);
    if (!confirmed) {
      appendSwitcherLog("Version switch cancelled.");
      return;
    }

    state.running = true;
    state.phases.rebase = { state: "idle", message: "Starting the version switch..." };
    render();

    appendSwitcherLog(`Switching to ${chosen.label} (${chosen.imageName})...`);

    try {
      await backend().RebaseImage(chosen.imageName);
      state.running = false;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `Rebase to ${chosen.label} completed. Reboot now to apply the new image.`;
      appendLog("Version switch completed. Reboot ready.");
      render();
    } catch (error) {
      state.running = false;
      appendSwitcherLog(error?.message || String(error));
      render();
    }
  });
}

function bindRuntimeEvents() {
  if (!window.runtime?.EventsOn) {
    return;
  }

  window.runtime.EventsOn("setup:phase", (payload) => {
    state.phases[payload.id] = {
      state: payload.state,
      message: payload.message,
    };

    if (payload.id === "first-run" && payload.state === "running") {
      appendLog("Launching a terminal window for ujust first-run.");
      appendLog("Complete the prompts there, then return here when it closes.");
    } else if (payload.id === "upgrade" && payload.state === "running") {
      appendLog("Launching a terminal window for ujust upgrade.");
      appendLog("Complete the prompts there, then return here when it closes.");
    } else if (payload.id === "rebase" && payload.state === "running") {
      appendSwitcherLog("Launching a terminal window for the version switch.");
      appendSwitcherLog("Authorize the polkit prompt and wait for the rebase to finish.");
    } else if (state.launchCompleted) {
      appendSwitcherLog(payload.message);
    } else {
      appendLog(payload.message);
    }

    render();
  });
}

async function loadProfile() {
  const profile = await backend().GetProfile();
  state.profile = profile;
}
function renderSwitcher() {
  const { currentImage, availableImages, selectedImage } = state;
  const grid = elements.imageGrid;

  grid.innerHTML = "";

  for (const img of availableImages) {
    const isCurrent = img.imageName === currentImage;
    const isSelected = img.imageName === selectedImage;

    const card = document.createElement("div");
    card.className = "image-card";
    if (isCurrent) card.classList.add("is-current");
    if (isSelected) card.classList.add("is-selected");
    card.dataset.imageName = img.imageName;

    const info = document.createElement("div");
    info.innerHTML = `<strong>${escHtml(img.label)}</strong>` +
      `<p class="image-description">${escHtml(img.description)}</p>`;

    const badges = document.createElement("div");
    badges.className = "image-badges";

    if (isCurrent) {
      const badge = document.createElement("span");
      badge.className = "image-badge current-badge";
      badge.textContent = "Currently using this image";
      badges.appendChild(badge);
    }

    if (img.recommended) {
      const badge = document.createElement("span");
      badge.className = "image-badge recommended-badge";
      badge.textContent = "Recommended";
      badges.appendChild(badge);
    }

    info.appendChild(badges);
    card.appendChild(info);
    grid.appendChild(card);

    card.addEventListener("click", () => {
      if (state.running) return;
      state.selectedImage = img.imageName;

      const isSame = img.imageName === currentImage;
      elements.sameImageBanner.classList.toggle("is-hidden", !isSame);
      elements.switchButton.disabled = isSame;

      if (isSame) {
        elements.rebaseDetails.classList.add("is-hidden");
      } else {
        elements.rebaseDetails.classList.remove("is-hidden");
        elements.rebaseTargetName.textContent = img.imageName;
      }

      renderSwitcher();
    });
  }

  const initialIsSame = selectedImage === currentImage;
  elements.sameImageBanner.classList.toggle("is-hidden", !initialIsSame);
  elements.switchButton.disabled = !selectedImage || initialIsSame || state.running;

  if (!initialIsSame && selectedImage) {
    elements.rebaseDetails.classList.remove("is-hidden");
    const chosen = availableImages.find((img) => img.imageName === selectedImage);
    if (chosen) elements.rebaseTargetName.textContent = chosen.imageName;
  } else {
    elements.rebaseDetails.classList.add("is-hidden");
  }
}

function escHtml(str) {
  const div = document.createElement("div");
  div.textContent = str;
  return div.innerHTML;
}

async function refreshNetwork() {
  state.checkingNetwork = true;
  render();

  try {
    state.networkReady = await backend().HasNetworkConnection();
  } catch (error) {
    state.networkReady = false;
  } finally {
    state.checkingNetwork = false;
    render();
  }
}

function render() {
  const actionsDisabled = state.running || !state.networkReady;

  elements.upgradeButton.disabled = actionsDisabled;
  elements.runButton.disabled = actionsDisabled;
  elements.networkBanner.classList.toggle("is-hidden", state.networkReady || state.checkingNetwork);
  elements.upgradeButton.textContent = state.running ? "Updating..." : "Update Caracal";
  elements.runButton.textContent = state.running ? "Running Setup..." : "Run First-Run";
  elements.rebootButton.disabled = state.running;

  if (state.launchCompleted) {
    const rebasePhase = state.phases.rebase || { state: "idle" };
    updatePill(elements.switcherPill, rebasePhase.state === "running" ? "running" : rebasePhase.state === "complete" ? "success" : "neutral");
    elements.switcherPill.textContent = rebasePhase.state === "running" ? "Running" : rebasePhase.state === "complete" ? "Complete" : "Idle";
  } else {
    updatePill(elements.runPill, state.running ? "running" : state.completed ? "success" : "neutral");
    elements.runPill.textContent = state.running ? "Running" : state.completed ? "Complete" : "Idle";
  }

  const activeStep = state.currentPage;

  for (const [key, page] of Object.entries(elements.pages)) {
    page.classList.toggle("is-hidden", key !== state.currentPage);
  }

  for (const item of elements.stepItems) {
    const key = item.dataset.step;
    const phase = state.phases[key] || { state: "idle" };
    item.classList.toggle("is-active", key === activeStep);
    item.classList.toggle("is-complete", phase.state === "complete" || phase.state === "ready");
    item.classList.toggle("is-error", phase.state === "error");
  }

  for (const card of elements.progressCards) {
    const key = card.dataset.progress;
    const phase = state.phases[key] || { state: "idle", message: "" };
    card.classList.toggle("is-running", phase.state === "running");
    card.classList.toggle("is-complete", phase.state === "complete");
    card.classList.toggle("is-ready", phase.state === "ready");
    card.classList.toggle("is-error", phase.state === "error");
    const copy = card.querySelector(".progress-copy");
    if (copy && phase.message) {
      copy.textContent = phase.message;
    }
  }
}

function updatePill(element, stateName) {
  element.classList.remove("neutral", "running", "success", "error");
  switch (stateName) {
    case "running":
      element.classList.add("running");
      break;
    case "complete":
    case "ready":
    case "success":
      element.classList.add("success");
      break;
    case "error":
      element.classList.add("error");
      break;
    default:
      element.classList.add("neutral");
      break;
  }
}

function appendLog(message) {
  const line = `[${new Date().toLocaleTimeString()}] ${message}`;
  elements.log.textContent = elements.log.textContent
    ? `${elements.log.textContent}\n${line}`
    : line;
  elements.log.scrollTop = elements.log.scrollHeight;
}

boot().catch((error) => {
  appendLog(error?.message || String(error));
});
