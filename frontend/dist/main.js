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
};

const elements = {
  stepItems: [...document.querySelectorAll(".step-chip")],
  progressCards: [...document.querySelectorAll(".progress-item")],
  pages: {
    "first-run": document.querySelector("#page-first-run"),
    finish: document.querySelector("#page-finish"),
  },
  upgradeButton: document.querySelector("#upgrade-button"),
  runButton: document.querySelector("#run-button"),
  networkBanner: document.querySelector("#network-banner"),
  rebootButton: document.querySelector("#reboot-button"),
  finishSummary: document.querySelector("#finish-summary"),
  log: document.querySelector("#log"),
  runPill: document.querySelector("#run-pill"),
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
  await refreshNetwork();
  window.setInterval(refreshNetwork, 10000);
  render();
  appendLog("Wizard ready.");
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

  updatePill(elements.runPill, state.running ? "running" : state.completed ? "success" : "neutral");
  elements.runPill.textContent = state.running ? "Running" : state.completed ? "Complete" : "Idle";

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
