// Share the demo's preference so moving between the site and demo keeps the theme.
export const themeBootstrap = `(function(){var t;try{t=localStorage.getItem('9lives-demo-theme')}catch{}var explicit=t==='dark'||t==='light';document.documentElement.dataset.theme=explicit?t:matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';document.documentElement.dataset.themePreference=explicit?'manual':'system'})()`;

