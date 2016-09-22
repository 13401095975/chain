import React from 'react'
import PageHeader from "../PageHeader/PageHeader"
import {
  TextField,
  JsonField,
  KeyConfiguration,
  ErrorBanner
} from "../Common"
import { reduxForm } from 'redux-form'

class Form extends React.Component {
  constructor(props) {
    super(props)

    this.submitWithErrors = this.submitWithErrors.bind(this)
  }

  submitWithErrors(data) {
    return new Promise((resolve, reject) => {
      this.props.submitForm(data)
        .catch((err) => reject({_error: err.message}))
    })
  }

  render() {
    const {
      fields: { alias, tags, definition, root_xpubs, quorum },
      error,
      handleSubmit,
      submitting
    } = this.props

    return(
      <div className='form-container'>
        <PageHeader title="New Asset" />

        <form onSubmit={handleSubmit(this.submitWithErrors)}>
          <TextField title='Alias' placeholder='Alias' fieldProps={alias} />
          <JsonField title='Tags' fieldProps={tags} />
          <JsonField title='Definition' fieldProps={definition} />
          <KeyConfiguration xpubs={root_xpubs} quorum={quorum} mockhsmKeys={this.props.mockhsmKeys}/>

          {error && <ErrorBanner
            title="There was a problem creating your asset:"
            message={error}/>}

          <button type='submit' className='btn btn-primary' disabled={submitting}>
            Submit
          </button>
        </form>
      </div>
    )
  }
}

const validate = values => {
  const errors = {}

  const jsonFields = ['tags', 'definition']
  jsonFields.forEach(key => {
    const fieldError = JsonField.validator(values[key])
    if (fieldError) { errors[key] = fieldError }
  })

  return errors
}

const fields = [ 'alias', 'tags', 'definition', 'root_xpubs[]', 'quorum' ]
export default reduxForm({
  form: 'newAssetForm',
  fields,
  validate,
  initialValues: {
    tags: '{\n\t\n}',
    definition: '{\n\t\n}',
  }
})(Form)
