import React from 'react'
import { Provider } from 'react-redux'
import { Router } from 'react-router'
import { history } from './utility/environment'
import { syncHistoryWithStore } from 'react-router-redux'

import routes from './routes'

export default class Root extends React.Component {
  componentWillMount() {
    document.title = 'Chain Core Dashboard'
  }

  render() {
    const store = this.props.store
    const syncedHistory = syncHistoryWithStore(history, store)
    return (
      <Provider store={store}>
        <Router history={syncedHistory} routes={routes} />
      </Provider>
    )
  }
}
