import React from 'react'
import styles from './TextField.scss'

const TEXT_FIELD_PROPS = [
  'value',
  'onBlur',
  'onChange',
  'onFocus',
]

class TextField extends React.Component {
  constructor(props) {
    super(props)
    this.state = {type: 'text'}
  }

  render() {
    // Select only valid props from Redux form field properties to
    // pass to input component
    const fieldProps = TEXT_FIELD_PROPS.reduce((mapping, key) => {
      if (this.props.fieldProps.hasOwnProperty(key)) mapping[key] = this.props.fieldProps[key]
      return mapping
    }, {})

    const inputClasses = ['form-control']
    const error = this.props.fieldProps.error
    if (error) {
      inputClasses.push(styles.errorInput)
    }

    return(
      <div>
        <div className='form-group'>
          {this.props.title && <label>{this.props.title}</label>}
          <input className='form-control'
            type={this.state.type}
            placeholder={this.props.placeholder}
            autoFocus={!!this.props.autoFocus}
            {...fieldProps} />

          {this.props.hint && <span className='help-block'>{this.props.hint}</span>}
        </div>

        {error && <span className={styles.errorText}>{error}</span>}
      </div>
    )
  }
}

export default TextField
