import { mapStateToProps, mapDispatchToProps, connect } from '../Base/New'
import Form from '../../features/assets/components/Form'

const type = 'asset'

const props = (state) => ({
  ...mapStateToProps(type)(state),
  mockhsmKeys: Object.keys(state.mockhsm.items).map(k => state.mockhsm.items[k])
})

export default connect(
  props,
  mapDispatchToProps(type),
  Form
)
