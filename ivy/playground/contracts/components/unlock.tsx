// external imports
import * as React from 'react'
import DocumentTitle from 'react-document-title'
import { connect } from 'react-redux'
import { Modal } from 'react-modal'

// ivy imports
import Section from '../../app/components/section'
import { Contract } from '../types'
import { getError, getContractMap, getSpendContractId, isFirstTime } from '../../contracts/selectors'

// internal imports
import SpendInputs from './argsDisplay'
import UnlockButton from './unlockButton'
import { DisplaySpendContract } from './display'
import UnlockDestination from './unlockDestination'
import ClauseSelect from './clauseselect'
import { ClauseValue, ClauseParameters, getWidget } from './parameters'
import { ContractValue } from './argsDisplay'

const mapStateToProps = (state) => {
  const error = getError(state)
  const map = getContractMap(state)
  const id = getSpendContractId(state)
  const firstTime = isFirstTime(state)
  const display = map[id] !== undefined
  return { error, display, firstTime }
}

const ErrorAlert = (props: { error: string }) => {
  return (
    <div style={{margin: '25px 0 0 0'}} className="alert alert-danger" role="alert">
      <span className="sr-only">Error:</span>
      <span className="glyphicon glyphicon-exclamation-sign" style={{marginRight: "5px"}}></span>
      {props.error}
    </div>
  )
}

export const Unlock = ({ error, display, firstTime }) => {
  let summary = (<div className="table-placeholder">No Contract Found</div>)
  let details = (<div className="table-placeholder">No Details Found</div>)
  let button

  if (display) {
    summary = (
      <div className="form-wrapper with-subsections">
        <section>
          <h4>Contract Template</h4>
          <DisplaySpendContract />
        </section>
        <ContractValue />
        <SpendInputs />
      </div>
    )

    details = (
      <div className="form-wrapper with-subsections">
        <ClauseSelect />
        <ClauseValue />
        <ClauseParameters />
        <UnlockDestination />
        { error ? <ErrorAlert error={error} /> : <div/> }
      </div>
    )

    button = (
      <UnlockButton />
    )
  }

  return (
    <DocumentTitle title="Unlock Value">
      <div>
        <Section name="Contract Summary">
          {summary}
        </Section>
        <Section name="Unlocking Details">
          {details}
        </Section>
        {button}
      </div>
    </DocumentTitle>
  )
}

export default connect(
  mapStateToProps
)(Unlock)
