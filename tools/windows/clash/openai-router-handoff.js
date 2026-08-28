// OpenAI and CPA Antigravity share one local handoff group. DIRECT is first
// because the OnePlus router owns the matching DNS and selective proxy path.

function uniq(arr) {
  const out = [];
  const seen = new Set();
  for (const x of arr) {
    if (!x || seen.has(x)) continue;
    seen.add(x);
    out.push(x);
  }
  return out;
}

function namesByKeyword(proxies, kw) {
  return proxies
    .map((p) => (p && p.name ? String(p.name) : ""))
    .filter((n) => n.includes(kw) && !n.includes("旧节点"));
}

function preferTypes(byName, names, typeOrder) {
  const rank = (n) => {
    const t = byName[n] && byName[n].type ? String(byName[n].type) : "";
    const i = typeOrder.indexOf(t);
    return i >= 0 ? i : 99;
  };
  return names.slice().sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
}

const aiGroupName = "AI-OpenAI-Antigravity";

// Keep this list identical to router-stack/openai-domains.txt. The phone
// generates DOMAIN-SUFFIX rules and trusted DNS answers from the same list.
const routerDomains = [
  "openai.com",
  "api.openai.com",
  "auth.openai.com",
  "auth0.openai.com",
  "platform.openai.com",
  "files.openai.com",
  "chat.openai.com",
  "ios.chat.openai.com",
  "android.chat.openai.com",
  "chatgpt.com",
  "ab.chatgpt.com",
  "cdn.oaistatic.com",
  "persistent.oaistatic.com",
  "oaistatic.com",
  "files.oaiusercontent.com",
  "oaiusercontent.com",
  "cloudcode-pa.googleapis.com",
  "daily-cloudcode-pa.googleapis.com",
  "daily-cloudcode-pa.sandbox.googleapis.com",
  "oauth2.googleapis.com",
  "www.googleapis.com",
  "antigravity-hub-auto-updater-974169037036.us-central1.run.app",
];

function main(config, profileName) {
  const proxies = Array.isArray(config.proxies) ? config.proxies : [];
  const byName = {};
  for (const p of proxies) {
    if (p && p.name) byName[String(p.name)] = p;
  }

  const jp = preferTypes(byName, namesByKeyword(proxies, "日本"), [
    "anytls",
    "vless",
    "trojan",
    "ss",
  ]);
  const jpPreferred = uniq([
    "🇯🇵 日本 | 01",
    "🇯🇵 日本 | 03",
    "🇯🇵 日本05 | 直连 | 移动",
    "🇯🇵 日本07 | 直连 | 三网优化",
    "🇯🇵 日本 | 04",
    "🇯🇵 日本06 | 直连",
    ...jp,
  ]).filter((n) => byName[n]);

  const groups = Array.isArray(config["proxy-groups"])
    ? config["proxy-groups"].filter(
        (group) =>
          !group ||
          typeof group.name !== "string" ||
          !group.name.startsWith("AI-"),
      )
    : [];
  groups.unshift({
    name: aiGroupName,
    type: "select",
    proxies: uniq(["DIRECT", ...jpPreferred]),
  });
  config["proxy-groups"] = groups;

  if (config["tcp-concurrent"] === undefined) config["tcp-concurrent"] = true;
  if (config["keep-alive-interval"] === undefined) {
    config["keep-alive-interval"] = 15;
  }

  // DIRECT must resolve every shared domain through the router's trusted DNS.
  // Public mainland resolvers return poisoned addresses for these domains.
  const dns = config.dns && typeof config.dns === "object" ? config.dns : {};
  const nameserverPolicy =
    dns["nameserver-policy"] && typeof dns["nameserver-policy"] === "object"
      ? dns["nameserver-policy"]
      : {};
  for (const domain of routerDomains) {
    delete nameserverPolicy[domain];
    nameserverPolicy[`+.${domain}`] = "192.168.2.1";
  }
  dns["nameserver-policy"] = nameserverPolicy;
  config.dns = dns;

  const aiRules = routerDomains.map(
    (domain) => `DOMAIN-SUFFIX,${domain},${aiGroupName}`,
  );
  const oldRules = Array.isArray(config.rules) ? config.rules : [];
  config.rules = [
    ...aiRules,
    ...oldRules.filter((rule) => {
      const target = String(rule).split(",").at(-1);
      return !target || !target.startsWith("AI-");
    }),
  ];
  return config;
}
