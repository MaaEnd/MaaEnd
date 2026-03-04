import type { FullConfig } from '@nekosu/maa-tools'

const config: FullConfig = {
  cwd: import.meta.dirname,

  maaVersion: 'latest',

  check: {
    interfacePath: 'assets/interface.json',
    override: {
      'mpe-config': 'error',
    },
  },
}

export default config
