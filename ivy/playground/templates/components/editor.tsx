// external imports
import * as React from 'react'
import { connect } from 'react-redux'

// internal imports
import Ace from './ace'
import ErrorAlert from './errorAlert'
import NewTemplate from './newTemplate'
import LoadTemplate from './loadTemplate'
import SaveTemplate from './saveTemplate'
import Opcodes from './opcodes'
import { getCompiled, getSource } from '../selectors'

// Handles syntax highlighting
require('../util/ivymode.js')

const mapStateToProps = (state) => {
  return {
    compiled: getCompiled(state),
    source: getSource(state)
  }
}

const Editor = ({ compiled, source }) => {
  return (
    <div>
      <div className="panel panel-default">
        <div className="panel-heading clearfix">
          <h1 className="panel-title pull-left">Contract Template</h1>
          <ul className="panel-heading-btns pull-right">
            <li><NewTemplate /></li>
            <li><LoadTemplate /></li>
            <li><SaveTemplate /></li>
          </ul>
        </div>
        <Ace source={source} />
        { compiled && compiled.error !== "" ? <ErrorAlert errorMessage={compiled.error} /> : <Opcodes />}
      </div>
    </div>
  )
}

export default connect(
  mapStateToProps,
)(Editor)
