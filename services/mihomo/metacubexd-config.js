(function () {
  const backendURL = window.location.origin + '/mihomo-api'
  const setupVersion = 'op7p-router-mihomo-v2'

  if (window.localStorage.getItem('op7p-router-setup') !== setupVersion) {
    window.localStorage.removeItem('selectedEndpoint')
    window.localStorage.removeItem('endpointList')
    window.localStorage.setItem('op7p-router-setup', setupVersion)
  }

  window.__METACUBEXD_CONFIG__ = {
    defaultBackendURL: backendURL,
    githubToken: '',
  }

  window.metacubexd = window.metacubexd || {}
  window.metacubexd.endpoint = {
    url: backendURL,
    secret: '',
  }
})()
