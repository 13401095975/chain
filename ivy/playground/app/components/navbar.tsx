// external imports
import * as React from 'react'
import { connect } from 'react-redux'
import { Link } from 'react-router-dom'
import ReactTooltip from 'react-tooltip'

// ivy imports
import { prefixRoute } from '../../core'

// internal imports
import Reset from './reset'
import Seed from './seed'

const logo = require('../../static/images/logo.png')
const symbol = require('../../static/images/chain-symbol.svg')

const mapStateToProps = (state) => {
  const location = state.routing.location
  if (!location) {
    return { path: 'lock' }
  }

  const pathname = location.pathname.split("/")
  if (pathname[1] === "ivy") {
    pathname.shift()
  }
  return { path: pathname[1] }
}

const Navbar = (props: { path: string }) => {
  return (
    <nav className="navbar navbar-inverse navbar-static-top navbar-fixed-top">
      <div className="container fixedcontainer">
        <div className="navbar-header">
          <a className="navbar-brand" href={prefixRoute('/')}>
            <img src={logo} />
          </a>
        </div>
        <ReactTooltip id="seedButtonTooltip" place="bottom" type="error" effect="solid"/>
        <ul className="nav navbar-nav navbar-right">
          <li className={props.path === 'unlock' ? '' : 'active'} ><Link to={prefixRoute('/')}>Lock Value</Link></li>
          <li className={props.path === 'unlock' ? 'active' : ''} ><Link to={prefixRoute('/unlock')}>Unlock Value</Link></li>
          <li className="divider-vertical"></li>
          <li><a href="https://chain.com/docs/1.2/ivy-playground/docs" target="_blank">Docs</a></li>
          <li><a href="https://chain.com/docs/1.2/ivy-playground/tutorial" target="_blank">Tutorial</a></li>
          <li><a href="../dashboard" target="_blank">Dashboard</a></li>
          <li className="dropdown">
            <a href="#" className="dropdown-toggle" data-toggle="dropdown" role="button" aria-haspopup="true" aria-expanded="false">Setup <span className="caret"></span></a>
            <ul className="dropdown-menu">
              {/* Reset and Seed return <li> elements */}
              <Reset />
              <Seed />
            </ul>
          </li>
        </ul>
        <div className="welcome" hidden>
        <div className="welcome-content">
          <img src={symbol}/>
          <h1>Welcome to Ivy Playground!</h1>
          <p>We've seeded your Chain Core with a few accounts and assets to help you get started. You can create more by visiting the Dashboard.</p>
          <p>The <a href="https:/chain.com/docs/1.2/ivy-playground/tutorial" target="_blank">tutorial</a> is a great place to start, which you can visit any time by clicking the link in the top right. Enjoy!</p>
          <button className="btn btn-primary btn-xl">Let's Go!</button>
        </div>
        <div className="welcome-screen-block"></div>
         </div>
      </div>
    </nav>
  )
}

export default connect(
  mapStateToProps
)(Navbar)
