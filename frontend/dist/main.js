const state = {
  profile: null,
  changeAccount: false,
  currentPage: "account",
  running: false,
  completed: false,
  phases: {
    account: { state: "idle", message: "This step is skipped unless you choose to change the account." },
    "first-run": { state: "idle", message: "The mandatory setup has not started yet." },
    finish: { state: "idle", message: "Reboot becomes available after setup finishes." },
  },
};

const elements = {
  stepItems: [...document.querySelectorAll(".step-chip")],
  progressCards: [...document.querySelectorAll(".progress-item")],
  pages: {
    account: document.querySelector("#page-account"),
    "first-run": document.querySelector("#page-first-run"),
    finish: document.querySelector("#page-finish"),
  },
  currentUsername: document.querySelector("#current-username"),
  currentHome: document.querySelector("#current-home"),
  skipChoice: document.querySelector("#skip-choice"),
  changeChoice: document.querySelector("#change-choice"),
  usernameInput: document.querySelector("#username-input"),
  passwordInput: document.querySelector("#password-input"),
  confirmPasswordInput: document.querySelector("#confirm-password-input"),
  accountHint: document.querySelector("#account-hint"),
  nextButton: document.querySelector("#next-button"),
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
  render();
  appendLog("Wizard ready.");
}

function bindEvents() {
  elements.skipChoice.addEventListener("click", () => {
    state.changeAccount = false;
    render();
  });

  elements.changeChoice.addEventListener("click", () => {
    state.changeAccount = true;
    render();
    elements.usernameInput.focus();
  });

  elements.nextButton.addEventListener("click", async () => {
    if (state.running) {
      return;
    }

    const request = buildRequest();
    if (!request) {
      return;
    }

    state.currentPage = "first-run";
    state.running = true;
    state.completed = false;
    state.phases["first-run"] = {
      state: "idle",
      message: "The terminal window will open once the account step is done.",
    };
    state.phases.finish = {
      state: "idle",
      message: "Reboot will unlock after the mandatory setup finishes.",
    };
    render();

    appendLog(request.changeAccount ? "Applying account changes before first-run setup..." : "Skipping account changes.");

    try {
      const result = await backend().RunSetup(request);
      state.running = false;
      state.completed = true;
      state.currentPage = "finish";
      elements.finishSummary.textContent = `First-run setup completed for ${result.appliedUsername}. Reboot now to finish applying the Caracal session changes.`;
      appendLog("Mandatory setup finished. Reboot is ready.");
      render();
    } catch (error) {
      state.running = false;
      if (state.phases.account.state === "error" && state.phases["first-run"].state !== "complete") {
        state.currentPage = "account";
      }
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
    } else {
      appendLog(payload.message);
    }

    render();
  });
}

async function loadProfile() {
  const profile = await backend().GetProfile();
  state.profile = profile;
  elements.currentUsername.textContent = profile.currentUsername || "Unknown user";
  elements.usernameInput.value = profile.currentUsername || "";
  elements.currentHome.textContent = profile.currentHome ? `• ${profile.currentHome}` : "";
}

function buildRequest() {
  const username = (elements.usernameInput.value || "").trim();
  const password = elements.passwordInput.value || "";
  const confirmPassword = elements.confirmPasswordInput.value || "";

  if (!state.changeAccount) {
    return {
      changeAccount: false,
      username: state.profile?.currentUsername || "",
      password: "",
    };
  }

  if (!username) {
    appendLog("Enter a username or skip this step.");
    return null;
  }

  if (!password) {
    appendLog("Enter a password or skip this step.");
    return null;
  }

  if (password !== confirmPassword) {
    appendLog("Password confirmation does not match.");
    return null;
  }

  return {
    changeAccount: true,
    username,
    password,
  };
}

function render() {
  elements.skipChoice.classList.toggle("is-active", !state.changeAccount);
  elements.changeChoice.classList.toggle("is-active", state.changeAccount);
  elements.skipChoice.setAttribute("aria-pressed", String(!state.changeAccount));
  elements.changeChoice.setAttribute("aria-pressed", String(state.changeAccount));

  const disableForm = state.running || !state.changeAccount;
  elements.usernameInput.disabled = disableForm;
  elements.passwordInput.disabled = disableForm;
  elements.confirmPasswordInput.disabled = disableForm;
  elements.accountHint.textContent = state.changeAccount
    ? "Choose the username and password that should be in place before ujust first-run launches."
    : "Skipping this step keeps the current account details.";

  elements.nextButton.disabled = state.running;
  elements.nextButton.textContent = state.running ? "Running Setup..." : "Next";
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
