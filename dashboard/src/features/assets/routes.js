import { RoutingContainer } from 'features/shared/components'
import { List, New, Show } from './components'

export default {
  path: 'assets',
  component: RoutingContainer,
  indexRoute: { component: List },
  childRoutes: [
    {
      path: 'create',
      component: New
    },
    {
      path: ':id',
      component: Show
    }
  ]
}
