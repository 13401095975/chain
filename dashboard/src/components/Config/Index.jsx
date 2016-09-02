import React from 'react'
import { TextField, ErrorBanner } from "../Common"
import InlineSVG from 'svg-inline-react'
import styles from './Index.scss'

export default class Index extends React.Component {
  constructor(props) {
    super(props)

    this.showNewFields = this.showNewFields.bind(this)
    this.showJoinFields = this.showJoinFields.bind(this)
    this.submitWithValidation = this.submitWithValidation.bind(this)
  }

  showNewFields() {
    return this.props.fields.is_generator.value === 'true'
  }

  showJoinFields() {
    return this.props.fields.is_generator.value === 'false'
  }

  submitWithValidation(data) {
    if (data.generator_url && !data.initial_block_hash) {
      return new Promise((_, reject) => reject({
        _error: "You must specifiy a blockchain ID to connect to a network"
      }))
    }

    return new Promise((resolve, reject) => {
      this.props.submitForm(data)
        .catch((err) => reject({_error: err.message}))
    })
  }

  render() {
    const {
      fields: {
        is_generator,
        generator_url,
        initial_block_hash
      },
      error,
      handleSubmit,
      submitting
    } = this.props

    let createNewIcon = require('../../images/config/create-new.svg')
    let joinExistingIcon = require('../../images/config/join-existing.svg')

    let submitButton = <button type="submit" className={`btn btn-primary btn-lg ${styles.submit}`} disabled={submitting}>
      <span className="glyphicon glyphicon-arrow-right" />
      &nbsp;{this.showNewFields() ? "Create" : "Join"} network
    </button>


    return (
      <form onSubmit={handleSubmit(this.submitWithValidation)}>
        <h2 className={styles.title}>Select how you would like to set up your blockchain</h2>
        <h3 className={styles.subtitle}>You can change your configuration at a later time</h3>

        {error && <ErrorBanner
          title="There was a problem configuring your core:"
          message={error}/>}

        <div className="row">
          <div className="col-sm-4">
            <label className={styles.choice_wrapper}>
              <input className={styles.choice_radio_button}
                    type="radio"
                    {...is_generator}
                    value='true'
                    checked={is_generator.value === 'true'} />
              <div className={styles.choice}>
                <InlineSVG src={require('!svg-inline!../../images/config/create-new.svg')} />
                <span className={styles.choice_title}>Create new blockchain network</span>

                <p>
                  Start a new blockchain network with this Chain Core as the block generator.
                </p>
              </div>
            </label>

            {this.showNewFields() && <div>
              {submitButton}
            </div>}
          </div>

          <div className="col-sm-4">
            <label className={styles.choice_wrapper}>
              <input className={styles.choice_radio_button}
                    type="radio"
                    {...is_generator}
                    value='false'
                    checked={is_generator.value === 'false'} />
              <div className={styles.choice}>
              <InlineSVG src={require('!svg-inline!../../images/config/join-existing.svg')} />
                <span className={styles.choice_title}>Join existing blockchain network</span>

                <p>
                  Connect this Chain Core to an existing blockchain network
                </p>
              </div>
            </label>

            {this.showJoinFields() && <div>
              <TextField
                title="Remote Generator URL"
                placeholder="https://<remote-chain-core>"
                fieldProps={generator_url} />
              <TextField
                title="Blockchain ID"
                placeholder="896a800000000000000"
                fieldProps={initial_block_hash} />

              {submitButton}
            </div>}
          </div>
        </div>
      </form>
    )
  }
}
